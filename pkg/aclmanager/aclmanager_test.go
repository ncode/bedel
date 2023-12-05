package aclmanager

import (
	"context"
	"fmt"
	"github.com/spf13/viper"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/stretchr/testify/assert"
)

var (
	primaryOutput = `
# Replication
role:master
connected_slaves:1
slave0:ip=172.21.0.3,port=6379,state=online,offset=322,lag=0
master_replid:1da7151855972ec8517bcae3d2c11454ff942d72
master_replid2:0000000000000000000000000000000000000000
master_repl_offset:322
second_repl_offset:-1
repl_backlog_active:1
repl_backlog_size:1048576
repl_backlog_first_byte_offset:1
repl_backlog_histlen:322`

	followerOutput = `
# Replication
role:slave
master_host:172.21.0.2
master_port:6379
master_link_status:up
master_last_io_seconds_ago:10
master_sync_in_progress:0
slave_repl_offset:434
slave_priority:100
slave_read_only:1
connected_slaves:0
master_replid:7d4b067fa70ad532ff7feff7bd7ff3cf27429b08
master_replid2:0000000000000000000000000000000000000000
master_repl_offset:434
second_repl_offset:-1
repl_backlog_active:1
repl_backlog_size:1048576
repl_backlog_first_byte_offset:1
repl_backlog_histlen:434`
)

func TestFindNodes(t *testing.T) {
	// Sample master and slave output for testing

	tests := []struct {
		name     string
		mockResp string
		want     map[string]int
		wantErr  bool
	}{
		{
			name:     "parse master output",
			mockResp: primaryOutput,
			want: map[string]int{
				"172.21.0.3:6379": Follower,
			},
			wantErr: false,
		},
		{
			name:     "parse Follower output",
			mockResp: followerOutput,
			want: map[string]int{
				"172.21.0.2:6379": Primary,
			},
			wantErr: false,
		},
		{
			name:     "error on replicationInfo",
			mockResp: followerOutput,
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			// Mocking the response for the Info function
			if tt.wantErr {
				mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
			} else {
				mock.ExpectInfo("replication").SetVal(tt.mockResp)
			}
			aclManager := AclManager{RedisClient: redisClient}
			ctx := context.Background()

			err := aclManager.findNodes(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("findNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			for address, function := range aclManager.nodes {
				if wantFunction, ok := tt.want[address]; ok {
					if wantFunction != function {
						t.Errorf("findNodes() wanted function %v not found", function)
					}
				}
				t.Errorf("findNodes() wanted address %v not found", address)
			}
		})
	}
}

func TestListAcls(t *testing.T) {
	tests := []struct {
		name     string
		mockResp []interface{}
		want     []string
		wantErr  bool
	}{
		{
			name: "parse valid ACL list",
			mockResp: []interface{}{
				"user default on nopass ~* &* +@all",
				"user alice on >password ~keys:* -@all +get +set +del",
			},
			want: []string{
				"user default on nopass ~* &* +@all",
				"user alice on >password ~keys:* -@all +get +set +del",
			},
			wantErr: false,
		},
		{
			name:     "empty ACL list",
			mockResp: []interface{}{},
			want:     []string(nil),
			wantErr:  false,
		},
		{
			name:     "nil response from Redis",
			mockResp: nil,
			want:     nil,
			wantErr:  false,
		},
		{
			name:     "error from Redis client",
			mockResp: nil,
			want:     nil,
			wantErr:  false,
		},
		{
			name: "non-string elements in ACL list",
			mockResp: []interface{}{
				"user default on nopass ~* &* +@all",
				123, // Invalid element
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			// Mocking the response for the ACL LIST command
			mock.ExpectDo("ACL", "LIST").SetVal(tt.mockResp)
			acls, err := listAcls(context.Background(), redisClient)

			if (err != nil) != tt.wantErr {
				t.Errorf("listAcls() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.want, acls)
		})
	}
}

func TestListAcls_Error(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()

	// Mocking the response for the ACL LIST command
	mock.ExpectDo("ACL", "LIST").SetVal([]string{"user acl1", "user acl2"})
	_, err := listAcls(context.Background(), redisClient)
	assert.Error(t, err)
}

func TestMirrorAcls(t *testing.T) {
	tests := []struct {
		name               string
		sourceAcls         []interface{}
		destinationAcls    []interface{}
		expectedDeleted    []string
		expectedAdded      []string
		listAclsError      error
		redisDoError       error
		wantSourceErr      bool
		wantDestinationErr bool
	}{
		{
			name:            "ACLs synced with deletions",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl3"},
			expectedDeleted: []string{"acl3"},
			expectedAdded:   []string{"acl2"},
			wantSourceErr:   false,
		},
		{
			name:               "ACLs synced with Error om SETUSER",
			sourceAcls:         []interface{}{"user acl1", "user acl2"},
			destinationAcls:    []interface{}{"user acl1", "user acl3"},
			redisDoError:       fmt.Errorf("DELUSER"),
			wantDestinationErr: true,
		},
		{
			name:               "ACLs synced with Error on SETUSER",
			sourceAcls:         []interface{}{"user acl1", "user acl2"},
			destinationAcls:    []interface{}{"user acl1", "user acl3"},
			redisDoError:       fmt.Errorf("SETUSER"),
			wantSourceErr:      false,
			wantDestinationErr: true,
		},
		{
			name:            "No ACLs to delete",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl2"},
			expectedDeleted: nil,
			wantSourceErr:   false,
		},
		{
			name:          "Error listing source ACLs",
			listAclsError: fmt.Errorf("error listing source ACLs"),
			wantSourceErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceClient, sourceMock := redismock.NewClientMock()
			destinationClient, destMock := redismock.NewClientMock()

			if tt.listAclsError != nil && tt.wantSourceErr {
				sourceMock.ExpectDo("ACL", "LIST").SetErr(tt.listAclsError)
			} else {
				sourceMock.ExpectDo("ACL", "LIST").SetVal(tt.sourceAcls)
			}

			if tt.listAclsError != nil && tt.wantDestinationErr {
				destMock.ExpectDo("ACL", "LIST").SetErr(tt.listAclsError)
			} else {
				destMock.ExpectDo("ACL", "LIST").SetVal(tt.destinationAcls)
				if tt.expectedDeleted != nil {
					for _, username := range tt.expectedDeleted {
						if tt.wantDestinationErr && tt.redisDoError.Error() == "DELUSER" {
							destMock.ExpectDo("ACL", "DELUSER", username).SetErr(tt.redisDoError)
							continue
						}
						destMock.ExpectDo("ACL", "DELUSER", username).SetVal("OK")
					}
				}
				if tt.expectedAdded != nil {
					for _, username := range tt.expectedAdded {
						if tt.wantDestinationErr && tt.redisDoError.Error() == "SETUSER" {
							destMock.ExpectDo("ACL", "SETUSER", username).SetErr(tt.redisDoError)
							continue
						}
						destMock.ExpectDo("ACL", "SETUSER", username).SetVal("OK")
					}
				}
			}

			added, deleted, err := mirrorAcls(context.Background(), sourceClient, destinationClient)
			if err != nil {
				if tt.wantSourceErr {
					if tt.listAclsError != nil && !strings.Contains(err.Error(), tt.listAclsError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.listAclsError)
					}
					if tt.redisDoError != nil && !strings.Contains(err.Error(), tt.redisDoError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.redisDoError)
					}
				}
				if !tt.wantDestinationErr {
					if tt.listAclsError != nil && !strings.Contains(err.Error(), tt.listAclsError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.listAclsError)
					}
					if tt.redisDoError != nil && !strings.Contains(err.Error(), tt.redisDoError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.redisDoError)
					}
				}
			}
			if !reflect.DeepEqual(deleted, tt.expectedDeleted) {
				t.Errorf("mirrorAcls() deleted = %v, expectedDeleted %v", deleted, tt.expectedDeleted)
			}
			if !reflect.DeepEqual(added, tt.expectedAdded) {
				t.Errorf("mirrorAcls() added = %v, expectedAdded %v", deleted, tt.expectedDeleted)
			}
		})
	}
}

func TestIsItPrimary(t *testing.T) {
	// Sample Primary and Follower output for testing

	tests := []struct {
		name     string
		mockResp string
		want     int
		wantErr  bool
	}{
		{
			name:     "parse Primary output",
			mockResp: primaryOutput,
			want:     Primary,
			wantErr:  false,
		},
		{
			name:     "parse Follower output",
			mockResp: followerOutput,
			want:     Follower,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			// Mocking the response for the Info function
			mock.ExpectInfo("replication").SetVal(tt.mockResp)
			aclManager := AclManager{RedisClient: redisClient}
			ctx := context.Background()
			nodes, err := aclManager.CurrentFunction(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("findNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.want, nodes)
		})
	}
}

func TestNewAclManager(t *testing.T) {
	tests := []struct {
		name string
		want *AclManager
	}{
		{
			name: "create AclManager",
			want: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.want.Addr, tt.want.Username, tt.want.Password)
			assert.Equal(t, tt.want.Addr, got.Addr)
			assert.Equal(t, tt.want.Username, got.Username)
			assert.Equal(t, tt.want.Password, got.Password)
			assert.NotNil(t, got.RedisClient)
		})
	}
}

func TestCurrentFunction_Error(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()

	// Mocking the response for the Info function
	mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
	aclManager := AclManager{RedisClient: redisClient}
	ctx := context.Background()

	_, err := aclManager.CurrentFunction(ctx)
	assert.Error(t, err)
}

func TestAclManager_Loop(t *testing.T) {
	viper.Set("syncInterval", 4)
	tests := []struct {
		name        string
		aclManager  *AclManager
		wantErr     bool
		expectError error
	}{
		{
			name: "Primary node",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
			wantErr: false,
		},
		{
			name: "Primary node with error",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
			wantErr:     true,
			expectError: fmt.Errorf("error"),
		},
		{
			name: "follower node with error",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
			wantErr:     true,
			expectError: fmt.Errorf("unable to check if it's a Primary"),
		},
		{
			name: "follower node",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
			wantErr:     false,
			expectError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()
			tt.aclManager.RedisClient = redisClient

			if tt.wantErr {
				if tt.name == "Primary node" {
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
				} else {
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetErr(fmt.Errorf("error"))
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
				}
			} else {
				if tt.name == "Primary node" {
					mock.ExpectInfo("replication").SetVal(primaryOutput)
					mock.ExpectInfo("replication").SetVal(primaryOutput)
					mock.ExpectInfo("replication").SetVal(primaryOutput)
					mock.ExpectInfo("replication").SetVal(primaryOutput)
					mock.ExpectInfo("replication").SetVal(primaryOutput)
					mock.ExpectInfo("replication").SetVal(primaryOutput)
					mock.ExpectInfo("replication").SetVal(primaryOutput)
				}
			}

			// Set up a cancellable context to control the loop
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Run Loop in a separate goroutine
			done := make(chan error, 1)
			go func() {
				done <- tt.aclManager.Loop(ctx)
			}()

			time.Sleep(time.Second * 10)

			// Cancel the context to stop the loop
			cancel()

			// Check for errors
			err := <-done
			if err != nil {
				if !tt.wantErr {
					t.Errorf("Expected no error, got: %v", err)
				}

				if !strings.Contains(err.Error(), tt.expectError.Error()) {
					t.Errorf("Expected error: %v, got: %v", tt.expectError, err)
				}
			}
		})
	}
}

func TestClose(t *testing.T) {
	redisClient, _ := redismock.NewClientMock()
	aclManager := AclManager{RedisClient: redisClient}
	err := aclManager.Close()
	assert.NoError(t, err)
}

func TestClosePanic(t *testing.T) {
	aclManager := AclManager{RedisClient: nil}
	assert.Panics(t, func() { aclManager.Close() })
}

//func BenchmarkParseRedisOutputFollower(b *testing.B) {
//	for i := 0; i < b.N; i++ {
//		_, err := parseRedisOutput(followerOutput)
//		if err != nil {
//			b.Fatal(err)
//		}
//	}
//}
//
//func BenchmarkParseRedisOutputMaster(b *testing.B) {
//	for i := 0; i < b.N; i++ {
//		_, err := parseRedisOutput(primaryOutput)
//		if err != nil {
//			b.Fatal(err)
//		}
//	}
//}
