package aclmanager

import (
	"context"
	"fmt"
	"github.com/spf13/viper"
	"reflect"
	"slices"
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
		nodes    map[string]int
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
		{
			name:     "ensure old nodes are removed",
			mockResp: primaryOutput,
			want: map[string]int{
				"172.21.0.3:6379": Follower,
			},
			wantErr: false,
			nodes: map[string]int{
				"192.168.0.1:6379": Follower,
				"192.168.0.2:6379": Follower,
				"192.168.0.3:6379": Follower,
				"192.168.0.4:6379": Follower,
			},
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
			aclManager := AclManager{RedisClient: redisClient, nodes: make(map[string]int)}
			ctx := context.Background()

			err := aclManager.findNodes(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("findNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.name == "ensure old nodes are removed" {
				for address, _ := range aclManager.nodes {
					if _, ok := tt.nodes[address]; ok {
						t.Errorf("findNodes() address %v shound not be found", address)
					}
				}
			}
			for address, function := range aclManager.nodes {
				if wantFunction, ok := tt.want[address]; ok {
					if wantFunction != function {
						t.Errorf("findNodes() wanted function %v not found", function)
					}
					return
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

func TestSyncAcls(t *testing.T) {
	tests := []struct {
		name               string
		sourceAcls         []interface{}
		destinationAcls    []interface{}
		expectedDeleted    []string
		expectedUpdated    []string
		listAclsError      error
		redisDoError       error
		saveAclError       error
		loadAclError       error
		wantSourceErr      bool
		wantDestinationErr bool
		aclFile            bool
	}{
		{
			name:            "ACLs synced with deletions",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl3"},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{"acl2"},
		},
		{
			name:            "ACLs synced with differences",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1 something something", "user acl3"},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{"acl1", "acl2"},
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
			destinationAcls:    []interface{}{"user acl1"},
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
		{
			name:               "Error listing destination ACLs",
			listAclsError:      fmt.Errorf("error listing destination ACLs"),
			wantDestinationErr: true,
		},
		{
			name:          "Invalid aclManagerPrimary",
			listAclsError: fmt.Errorf("error listing destination ACLs"),
		},
		{
			name:            "ACLs synced with deletions, aclFile",
			sourceAcls:      []interface{}{"user acl1", "user acl2"},
			destinationAcls: []interface{}{"user acl1", "user acl3"},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{"acl2"},
			aclFile:         true,
		},
		{
			name:          "Error on save ACL file on primary, aclFile",
			sourceAcls:    []interface{}{"user acl1", "user acl2"},
			saveAclError:  fmt.Errorf("failed to save ACL on primary"),
			aclFile:       true,
			wantSourceErr: true,
		},
		{
			name:               "Error on save ACL file on destination, aclFile",
			sourceAcls:         []interface{}{"user acl1", "user acl2"},
			destinationAcls:    []interface{}{"user acl1", "user acl3"},
			saveAclError:       fmt.Errorf("failed to save ACL on destination"),
			aclFile:            true,
			wantDestinationErr: true,
		},
		{
			name:               "Error on load ACL file, aclFile",
			sourceAcls:         []interface{}{"user acl1", "user acl2"},
			destinationAcls:    []interface{}{"user acl1", "user acl3"},
			loadAclError:       fmt.Errorf("failed to load ACL"),
			aclFile:            true,
			wantDestinationErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primaryClient, sourceMock := redismock.NewClientMock()
			followerClient, destMock := redismock.NewClientMock()

			aclManagerPrimary := &AclManager{RedisClient: primaryClient, nodes: make(map[string]int)}
			aclManagerFollower := &AclManager{RedisClient: followerClient, nodes: make(map[string]int)}

			if tt.name == "Invalid aclManagerPrimary" {
				aclManagerPrimary = nil
				_, _, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
				assert.Error(t, err)
				assert.Equal(t, "no primary found", err.Error())
				return
			}

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
				if tt.expectedUpdated != nil {
					for _, username := range tt.expectedUpdated {
						if tt.wantDestinationErr && tt.redisDoError.Error() == "SETUSER" {
							destMock.ExpectDo("ACL", "SETUSER", username).SetErr(tt.redisDoError)
							continue
						}
						destMock.ExpectDo("ACL", "SETUSER", username).SetVal("OK")
					}
				}
			}

			if tt.listAclsError != nil && tt.wantDestinationErr {
				destMock.ExpectDo("ACL", "LIST").SetErr(tt.listAclsError)
			} else {
				destMock.ExpectDo("ACL", "LIST").SetVal(tt.destinationAcls)
			}

			if tt.aclFile {
				if tt.saveAclError != nil {
					sourceMock.ExpectDo("ACL", "SAVE").SetErr(tt.saveAclError)
					if !tt.wantSourceErr {
						destMock.ExpectDo("ACL", "SAVE").SetErr(tt.saveAclError)
					}
				} else {
					sourceMock.ExpectDo("ACL", "SAVE").SetVal("OK")
					destMock.ExpectDo("ACL", "SAVE").SetVal("OK")
				}

				if tt.loadAclError != nil && !tt.wantSourceErr {
					destMock.ExpectDo("ACL", "LOAD").SetErr(tt.loadAclError)
				} else {
					destMock.ExpectDo("ACL", "LOAD").SetVal("OK")
				}
			}

			added, deleted, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
			if err != nil {
				if tt.wantSourceErr {
					if tt.listAclsError != nil && !strings.Contains(err.Error(), tt.listAclsError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.listAclsError)
					}
					if tt.redisDoError != nil && !strings.Contains(err.Error(), tt.redisDoError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.redisDoError)
					}
				}
				if tt.wantDestinationErr {
					if tt.listAclsError != nil && !strings.Contains(err.Error(), tt.listAclsError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.listAclsError)
					}
					if tt.redisDoError != nil && !strings.Contains(err.Error(), tt.redisDoError.Error()) {
						t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.redisDoError)
					}
				}
				if !tt.wantSourceErr && !tt.wantDestinationErr {
					t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.wantSourceErr)
				}
			}
			slices.Sort(added)
			slices.Sort(tt.expectedUpdated)
			slices.Sort(deleted)
			slices.Sort(tt.expectedDeleted)
			if !reflect.DeepEqual(deleted, tt.expectedDeleted) {
				t.Errorf("mirrorAcls() deleted = %v, expectedDeleted %v", deleted, tt.expectedDeleted)
			}
			if !reflect.DeepEqual(added, tt.expectedUpdated) {
				t.Errorf("mirrorAcls() updated = %v, expectedUpdated %v", deleted, tt.expectedUpdated)
			}
		})
	}
}

func TestCurrentFunction(t *testing.T) {
	tests := []struct {
		name                 string
		mockResp             string
		want                 int
		wantErr              bool
		RedisExpectInfoError error
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
		{
			name:                 "parse primary error",
			mockResp:             primaryOutput,
			want:                 Unknown,
			wantErr:              true,
			RedisExpectInfoError: fmt.Errorf("error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			// Mocking the response for the Info function
			if tt.wantErr {
				mock.ExpectInfo("replication").SetErr(tt.RedisExpectInfoError)
			} else {
				mock.ExpectInfo("replication").SetVal(tt.mockResp)
			}
			aclManager := AclManager{RedisClient: redisClient, nodes: make(map[string]int)}
			ctx := context.Background()
			nodes, err := aclManager.CurrentFunction(ctx)
			if (err != nil) != tt.wantErr {
				if !strings.Contains(err.Error(), tt.RedisExpectInfoError.Error()) {
					t.Errorf("findNodes() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
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
			got := New(tt.want.Addr, tt.want.Username, tt.want.Password, false)
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

func TestAclManager_Primary(t *testing.T) {
	tests := []struct {
		name     string
		mockResp string
		want     string
		wantErr  bool
	}{
		{
			name:     "parse master output",
			mockResp: primaryOutput,
			wantErr:  false,
		},
		{
			name:     "parse Follower output",
			mockResp: followerOutput,
			want:     "172.21.0.2:6379",
			wantErr:  false,
		},
		{
			name:     "error on replicationInfo",
			mockResp: followerOutput,
			wantErr:  true,
		},
		{
			name:     "username and password",
			mockResp: followerOutput,
			wantErr:  false,
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
			aclManager := AclManager{RedisClient: redisClient, Username: "username", Password: "password", nodes: make(map[string]int)}
			ctx := context.Background()

			primary, err := aclManager.Primary(ctx)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, primary)
				return
			}

			if tt.name == "username and password" {
				assert.Equal(t, aclManager.Username, primary.Username)
				assert.Equal(t, aclManager.Password, primary.Password)
				return
			}

			assert.NoError(t, err)
			if tt.want == "" {
				assert.Nil(t, primary)
				return
			}
			assert.NotNil(t, primary)
			assert.Equal(t, tt.want, primary.Addr)
		})
	}
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
				aclFile:  false,
				nodes:    make(map[string]int),
			},
			wantErr: false,
		},
		{
			name: "Primary node with error",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
				aclFile:  false,
				nodes:    make(map[string]int),
			},
			wantErr:     true,
			expectError: fmt.Errorf("unable to find Primary"),
		},
		{
			name: "follower node with error",
			aclManager: &AclManager{
				Addr:     "localhost:6379",
				Password: "password",
				Username: "username",
				aclFile:  false,
				nodes:    make(map[string]int),
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
				aclFile:  false,
				nodes:    make(map[string]int),
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
				if tt.name == "Primary node with error" {
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

func TestSaveAclFile(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		err     error
	}{
		{
			name:    "successful ACL save",
			wantErr: false,
		},
		{
			name:    "error saving ACL to file",
			wantErr: true,
			err:     fmt.Errorf("failed to save ACL"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()
			if tt.wantErr {
				mock.ExpectDo("ACL", "SAVE").SetErr(tt.err)
			} else {
				mock.ExpectDo("ACL", "SAVE").SetVal("OK")
			}

			ctx := context.Background()
			err := saveAclFile(ctx, redisClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("saveAclFile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !strings.HasSuffix(err.Error(), tt.err.Error()) {
				t.Errorf("saveAclFile() got unexpected error = %v, want %v", err, tt.err)
			}

			assertExpectations(t, mock)
		})
	}
}

func assertExpectations(t *testing.T, mock redismock.ClientMock) {
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unmet expectations: %s", err)
	}
}

func TestLoadAclFile(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		err     error
	}{
		{
			name:    "successful ACL load",
			wantErr: false,
		},
		{
			name:    "error loading ACL from file",
			wantErr: true,
			err:     fmt.Errorf("failed to load ACL"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()
			if tt.wantErr {
				mock.ExpectDo("ACL", "LOAD").SetErr(tt.err)
			} else {
				mock.ExpectDo("ACL", "LOAD").SetVal("OK")
			}

			ctx := context.Background()
			err := loadAclFile(ctx, redisClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadAclFile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantErr && !strings.HasSuffix(err.Error(), tt.err.Error()) {
				t.Errorf("loadAclFile() got unexpected error = %v, want %v", err, tt.err)
			}

			assertExpectations(t, mock)
		})
	}
}

func TestFindNodes_LargeCluster(t *testing.T) {
	mockResp := generateLargeClusterOutput(1000) // Generates a mock output for 1000 nodes
	redisClient, mock := redismock.NewClientMock()
	mock.ExpectInfo("replication").SetVal(mockResp)

	aclManager := AclManager{RedisClient: redisClient, nodes: make(map[string]int)}
	ctx := context.Background()

	err := aclManager.findNodes(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 1000, len(aclManager.nodes))
}

func TestLoop_ShortInterval(t *testing.T) {
	viper.Set("syncInterval", 1) // Set a very short sync interval for testing
	redisClient, mock := redismock.NewClientMock()

	aclManager := &AclManager{
		Addr:        "localhost:6379",
		Password:    "password",
		Username:    "username",
		RedisClient: redisClient,
		nodes:       make(map[string]int),
	}

	mock.ExpectInfo("replication").SetVal(followerOutput)
	mock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user default on nopass ~* &* +@all",
	})

	// Set up a cancellable context to control the loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run Loop in a separate goroutine
	done := make(chan error, 1)
	go func() {
		done <- aclManager.Loop(ctx)
	}()

	time.Sleep(time.Second * 5) // Run the loop for a few seconds

	// Cancel the context to stop the loop
	cancel()

	// Check for errors
	err := <-done
	assert.NoError(t, err)
}

func generateLargeClusterOutput(nodeCount int) string {
	var sb strings.Builder
	sb.WriteString("# Replication\nrole:master\nconnected_slaves:" + fmt.Sprint(nodeCount) + "\n")
	for i := 0; i < nodeCount; i++ {
		sb.WriteString(fmt.Sprintf("slave%d:ip=172.21.0.%d,port=6379,state=online,offset=322,lag=0\n", i, i+3))
	}
	return sb.String()
}

func TestSyncAcls_ACLFileEnabled(t *testing.T) {
	primaryClient, primaryMock := redismock.NewClientMock()
	followerClient, followerMock := redismock.NewClientMock()

	aclManagerPrimary := &AclManager{RedisClient: primaryClient, nodes: make(map[string]int), aclFile: true}
	aclManagerFollower := &AclManager{RedisClient: followerClient, nodes: make(map[string]int), aclFile: true}

	primaryMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})
	followerMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl3",
	})

	primaryMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "LOAD").SetVal("OK")

	updated, deleted, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"acl2"}, updated)
	assert.ElementsMatch(t, []string{"acl3"}, deleted)
}

func TestSyncAcls_SaveACLFileError(t *testing.T) {
	primaryClient, primaryMock := redismock.NewClientMock()
	followerClient, followerMock := redismock.NewClientMock()

	aclManagerPrimary := &AclManager{RedisClient: primaryClient, nodes: make(map[string]int), aclFile: true}
	aclManagerFollower := &AclManager{RedisClient: followerClient, nodes: make(map[string]int), aclFile: true}

	primaryMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})
	followerMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl3",
	})

	primaryMock.ExpectDo("ACL", "SAVE").SetErr(fmt.Errorf("failed to save ACL on primary"))

	_, _, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save ACL on primary")
}

func TestSyncAcls_LoadACLFileError(t *testing.T) {
	primaryClient, primaryMock := redismock.NewClientMock()
	followerClient, followerMock := redismock.NewClientMock()

	aclManagerPrimary := &AclManager{RedisClient: primaryClient, nodes: make(map[string]int), aclFile: true}
	aclManagerFollower := &AclManager{RedisClient: followerClient, nodes: make(map[string]int), aclFile: true}

	primaryMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})
	followerMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})

	primaryMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "LOAD").SetErr(fmt.Errorf("failed to load ACL on follower"))

	_, _, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load ACL on follower")
}

func TestSyncAcls_ACLFileSync(t *testing.T) {
	primaryClient, primaryMock := redismock.NewClientMock()
	followerClient, followerMock := redismock.NewClientMock()

	aclManagerPrimary := &AclManager{RedisClient: primaryClient, nodes: make(map[string]int), aclFile: true}
	aclManagerFollower := &AclManager{RedisClient: followerClient, nodes: make(map[string]int), aclFile: true}

	primaryMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})
	followerMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2", "user acl3",
	})
	followerMock.ExpectDo("ACL", "DELUSER", "acl3").SetVal("OK")

	primaryMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "LOAD").SetVal("OK")

	_, _, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
	assert.NoError(t, err)

	primaryMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})
	followerMock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user acl1", "user acl2",
	})

	primaryMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "SAVE").SetVal("OK")
	followerMock.ExpectDo("ACL", "LOAD").SetVal("OK")

	updated, deleted, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
	assert.NoError(t, err)
	assert.Empty(t, updated)
	assert.Empty(t, deleted)
}
