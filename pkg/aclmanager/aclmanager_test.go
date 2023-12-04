package aclmanager

import (
	"context"
	"fmt"
	"github.com/spf13/viper"
	"reflect"
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
		want     []NodeInfo
		wantErr  bool
	}{
		{
			name:     "parse master output",
			mockResp: primaryOutput,
			want: []NodeInfo{
				{
					Address:  "172.21.0.3:6379",
					Function: Follower,
				},
			},
			wantErr: false,
		},
		{
			name:     "parse Follower output",
			mockResp: followerOutput,
			want: []NodeInfo{
				{
					Address:  "172.21.0.2:6379",
					Function: Primary,
				},
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

			nodes, err := aclManager.FindNodes()
			if (err != nil) != tt.wantErr {
				t.Errorf("FindNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			assert.Equal(t, tt.want, nodes)
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
		name            string
		sourceAcls      []interface{}
		destinationAcls []interface{}
		expectedDeleted []string
		expectedAdded   []string
		listAclsError   error
		redisDoError    error
		wantErr         bool
	}{
		{
			name:            "ACLs synced with deletions",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl3"},
			expectedDeleted: []string{"acl3"},
			expectedAdded:   []string{"acl2"},
			wantErr:         false,
		},
		{
			name:            "ACLs synced with Error om SETUSER",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl3"},
			redisDoError:    fmt.Errorf("DELUSER"),
			wantErr:         true,
		},
		{
			name:            "ACLs synced with Error on SETUSER",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl3"},
			redisDoError:    fmt.Errorf("SETUSER"),
			wantErr:         true,
		},
		{
			name:            "No ACLs to delete",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl2"},
			expectedDeleted: nil,
			wantErr:         false,
		},
		{
			name:          "Error listing source ACLs",
			listAclsError: fmt.Errorf("error listing source ACLs"),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceClient, sourceMock := redismock.NewClientMock()
			destinationClient, destMock := redismock.NewClientMock()

			if tt.listAclsError != nil {
				sourceMock.ExpectDo("ACL", "LIST").SetErr(tt.listAclsError)
			} else {
				sourceMock.ExpectDo("ACL", "LIST").SetVal(tt.sourceAcls)
			}

			if tt.listAclsError != nil {
				destMock.ExpectDo("ACL", "LIST").SetErr(tt.listAclsError)
			} else {
				destMock.ExpectDo("ACL", "LIST").SetVal(tt.destinationAcls)
				if tt.expectedDeleted != nil {
					for _, acl := range tt.expectedDeleted {
						if tt.wantErr && tt.redisDoError.Error() == "DELUSER" {
							destMock.ExpectDo("ACL", "DELUSER", acl).SetErr(tt.redisDoError)
							continue
						}
						destMock.ExpectDo("ACL", "DELUSER", acl).SetVal("OK")
					}
				}
				if tt.expectedAdded != nil {
					for _, acl := range tt.expectedAdded {
						if tt.wantErr && tt.redisDoError.Error() == "SETUSER" {
							destMock.ExpectDo("ACL", "SETUSER", acl).SetErr(tt.redisDoError)
							continue
						}
						destMock.ExpectDo("ACL", "SETUSER", acl).SetVal("OK")
					}
				}
			}

			added, deleted, err := mirrorAcls(context.Background(), sourceClient, destinationClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.wantErr)
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

			nodes, err := aclManager.CurrentFunction()
			if (err != nil) != tt.wantErr {
				t.Errorf("FindNodes() error = %v, wantErr %v", err, tt.wantErr)
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

	_, err := aclManager.CurrentFunction()
	assert.Error(t, err)
}

func TestAclManager_Loop(t *testing.T) {
	viper.Set("syncInterval", 1)
	tests := []struct {
		name        string
		aclManager  *AclManager
		wantErr     bool
		expectError error
	}{
		{
			name: "primary node",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
			wantErr: false,
		},
		{
			name: "follower node",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
			},
			wantErr:     true,
			expectError: fmt.Errorf("error syncing acls"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()
			tt.aclManager.RedisClient = redisClient

			if tt.wantErr == false {
				if tt.name == "primary node" {
					mock.ExpectInfo("replication").SetVal(primaryOutput)
				} else {
					mock.ExpectInfo("replication").SetVal(followerOutput)
					mock.ExpectInfo("replication").SetVal(followerOutput)
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

			if tt.wantErr == false && tt.name == "follower node" {
				time.Sleep(time.Second * 10)
			}

			// Cancel the context to stop the loop
			cancel()

			// Check for errors
			err := <-done
			if err != nil {
				if !tt.wantErr {
					t.Errorf("Expected no error, got: %v", err)
				}

				if err.Error() != tt.expectError.Error() {
					t.Errorf("Expected error: %v, got: %v", tt.expectError, err)
				}
			}
		})
	}
}

func BenchmarkParseRedisOutputFollower(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := parseRedisOutput(followerOutput)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseRedisOutputMaster(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := parseRedisOutput(primaryOutput)
		if err != nil {
			b.Fatal(err)
		}
	}
}
