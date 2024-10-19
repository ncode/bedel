package aclmanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/stretchr/testify/assert"
)

// Sample outputs for ROLE command
var (
	primaryRoleOutput = []interface{}{
		"master",
		int64(0),
		[]interface{}{
			[]interface{}{"172.21.0.3", int64(6379)},
		},
	}

	followerRoleOutput = []interface{}{
		"slave",
		"172.21.0.2",
		int64(6379),
		"connected",
		int64(1),
	}
)

func TestFindNodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		mockRoleResp   interface{}
		expectedNodes  map[string]int
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name:         "parse primary role output",
			mockRoleResp: primaryRoleOutput,
			expectedNodes: map[string]int{
				"172.21.0.3:6379": Follower,
			},
			wantErr: false,
		},
		{
			name:         "parse follower role output",
			mockRoleResp: followerRoleOutput,
			expectedNodes: map[string]int{
				"172.21.0.2:6379": Primary,
			},
			wantErr: false,
		},
		{
			name:           "ROLE command returns empty result",
			mockRoleResp:   []interface{}{},
			expectedNodes:  nil,
			wantErr:        true,
			expectedErrMsg: "findNodes: ROLE command returned empty result",
		},
		{
			name: "ROLE command first element not a string",
			mockRoleResp: []interface{}{
				int64(12345), // Non-string type explicitly set to int64
				"some other data",
			},
			expectedNodes:  nil,
			wantErr:        true,
			expectedErrMsg: "findNodes: unexpected type for role: int64",
		},
		{
			name:           "error on ROLE command",
			mockRoleResp:   nil, // Simulate Redis error
			expectedNodes:  nil,
			wantErr:        true,
			expectedErrMsg: "findNodes: ROLE command failed",
		},
		{
			name: "unknown role type",
			mockRoleResp: []interface{}{
				"sentinel",
			},
			expectedNodes:  nil,
			wantErr:        true,
			expectedErrMsg: "findNodes: unknown role type: sentinel",
		},
		{
			name:           "unexpected type for roleInfo",
			mockRoleResp:   "invalid_type", // Not a slice
			expectedNodes:  nil,
			wantErr:        true,
			expectedErrMsg: "findNodes: unexpected type for roleInfo: string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			// Setup the expected ROLE command response
			if tt.wantErr && tt.mockRoleResp == nil {
				// Simulate an error from Redis
				mock.ExpectDo("ROLE").SetErr(fmt.Errorf("findNodes: ROLE command failed"))
			} else {
				// Simulate a successful ROLE command with the provided response
				mock.ExpectDo("ROLE").SetVal(tt.mockRoleResp)
			}

			// Initialize AclManager with the mocked Redis client
			aclManager := AclManager{
				RedisClient: redisClient,
				nodes:       make(map[string]int),
				mu:          sync.Mutex{},
			}
			ctx := context.Background()

			// Execute the findNodes function
			err := aclManager.findNodes(ctx)

			// Assert whether an error was expected
			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				assert.NoError(t, err)
				aclManager.mu.Lock()
				defer aclManager.mu.Unlock()
				assert.Equal(t, tt.expectedNodes, aclManager.nodes)
			}

			// Ensure all expectations were met
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestListAcls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		mockResp       interface{}
		expectedAcls   []string
		wantErr        bool
		expectedErrMsg string
	}{
		{
			name: "valid ACL list",
			mockResp: []interface{}{
				"user default on nopass ~* &* +@all",
				"user alice on >password ~keys:* -@all +get +set +del",
			},
			expectedAcls: []string{
				"user default on nopass ~* &* +@all",
				"user alice on >password ~keys:* -@all +get +set +del",
			},
			wantErr: false,
		},
		{
			name:         "empty ACL list",
			mockResp:     []interface{}{},
			expectedAcls: []string{},
			wantErr:      false,
		},
		{
			name:           "error from Redis client",
			mockResp:       nil,
			wantErr:        true,
			expectedErrMsg: "error",
		},
		{
			name: "aclList contains non-string element",
			mockResp: []interface{}{
				"user default on nopass ~* &* +@all",
				map[string]interface{}{
					"unexpected": "data",
				},
			},
			wantErr:        true,
			expectedErrMsg: "unexpected type for ACL: map[string]interface {}",
		},
		{
			name:           "result is not []interface{}",
			mockResp:       "invalid_type",
			wantErr:        true,
			expectedErrMsg: "unexpected result format: string",
		},
		{
			name:           "nil response",
			mockResp:       nil,
			wantErr:        true,
			expectedErrMsg: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			if tt.wantErr && tt.mockResp == nil && tt.expectedErrMsg == "error" {
				// Simulate an error from Redis
				mock.ExpectDo("ACL", "LIST").SetErr(fmt.Errorf("error"))
			} else {
				// Simulate a successful ACL LIST command with the provided response
				mock.ExpectDo("ACL", "LIST").SetVal(tt.mockResp)
			}

			acls, err := listAcls(context.Background(), redisClient)

			if (err != nil) != tt.wantErr {
				t.Errorf("listAcls() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedAcls, acls)
			}

			// Ensure all expectations were met
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestListAndMapAcls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		mockResp        interface{}
		expectedHashMap map[string]string
		expectedStrMap  map[string]string
		wantErr         bool
		expectedErrMsg  string
	}{
		{
			name: "valid ACL list",
			mockResp: []interface{}{
				"user default on nopass ~* &* +@all",
				"user alice on >password ~keys:* -@all +get +set +del",
			},
			expectedHashMap: map[string]string{
				"default": hashString("user default on nopass ~* &* +@all"),
				"alice":   hashString("user alice on >password ~keys:* -@all +get +set +del"),
			},
			expectedStrMap: map[string]string{
				"default": "user default on nopass ~* &* +@all",
				"alice":   "user alice on >password ~keys:* -@all +get +set +del",
			},
			wantErr: false,
		},
		{
			name:            "empty ACL list",
			mockResp:        []interface{}{},
			expectedHashMap: map[string]string{},
			expectedStrMap:  map[string]string{},
			wantErr:         false,
		},
		{
			name:           "error from Redis client",
			mockResp:       nil,
			wantErr:        true,
			expectedErrMsg: "error listing ACLs",
		},
		{
			name: "invalid ACL format",
			mockResp: []interface{}{
				"invalid_acl",
				"user alice on >password ~keys:* -@all +get +set +del",
			},
			expectedHashMap: map[string]string{
				"alice": hashString("user alice on >password ~keys:* -@all +get +set +del"),
			},
			expectedStrMap: map[string]string{
				"alice": "user alice on >password ~keys:* -@all +get +set +del",
			},
			wantErr: false,
		},
		{
			name:           "result is not []interface{}",
			mockResp:       "invalid_type",
			wantErr:        true,
			expectedErrMsg: "unexpected result format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			if tt.wantErr && tt.mockResp == nil {
				mock.ExpectDo("ACL", "LIST").SetErr(fmt.Errorf("error"))
			} else {
				mock.ExpectDo("ACL", "LIST").SetVal(tt.mockResp)
			}

			aclHashMap, aclStrMap, err := listAndMapAcls(context.Background(), redisClient)

			if (err != nil) != tt.wantErr {
				t.Errorf("listAndMapAcls() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedHashMap, aclHashMap)
				assert.Equal(t, tt.expectedStrMap, aclStrMap)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSyncAcls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		sourceAcls        interface{}
		destinationAcls   interface{}
		expectedDeleted   []string
		expectedUpdated   []string
		sourceListAclsErr error
		destListAclsErr   error
		redisDoError      error
		saveAclError      error
		loadAclError      error
		wantErr           bool
		expectedErrMsg    string
		aclFile           bool
	}{
		{
			name: "ACLs synced with deletions",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl2 on >password2 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl3 on >password3 ~* +@all",
			},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{"acl2"},
			aclFile:         false,
			wantErr:         false,
		},
		{
			name: "ACLs synced with differences",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl2 on >password2 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password_different ~* +@all",
			},
			expectedDeleted: []string{},
			expectedUpdated: []string{"acl1", "acl2"},
			aclFile:         false,
			wantErr:         false,
		},
		{
			name:              "Error listing source ACLs",
			sourceListAclsErr: fmt.Errorf("error listing source ACLs"),
			wantErr:           true,
			expectedErrMsg:    "SyncAcls: error listing source ACLs",
			aclFile:           false,
		},
		{
			name:            "Error listing destination ACLs",
			sourceAcls:      []interface{}{}, // Set to empty slice to prevent panic
			destListAclsErr: fmt.Errorf("error listing destination ACLs"),
			wantErr:         true,
			expectedErrMsg:  "SyncAcls: error listing destination ACLs",
			aclFile:         false,
		},
		{
			name: "Error deleting ACL",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl3 on >password3 ~* +@all",
			},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{},
			redisDoError:    fmt.Errorf("error deleting ACL"), // Simulate error only on deletion
			wantErr:         true,
			expectedErrMsg:  "SyncAcls: error executing pipeline",
			aclFile:         false,
		},
		{
			name: "Error setting ACL",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl2 on >password2 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			expectedDeleted: []string{},
			expectedUpdated: []string{"acl2"},
			redisDoError:    fmt.Errorf("error setting ACL"),
			wantErr:         true,
			expectedErrMsg:  "SyncAcls: error executing pipeline",
			aclFile:         false,
		},
		{
			name: "ACLs synced with ACL file enabled",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl2 on >password2 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl3 on >password3 ~* +@all",
			},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{"acl2"},
			aclFile:         true,
			wantErr:         false,
		},
		{
			name: "Error saving ACL file",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl3 on >password3 ~* +@all",
			},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{},
			aclFile:         true,
			saveAclError:    fmt.Errorf("error saving ACL file"),
			wantErr:         true,
			expectedErrMsg:  "SyncAcls: error saving ACLs to aclFile",
		},
		{
			name: "Error loading ACL file",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl2 on >password2 ~* +@all", // Added acl2
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
				"user acl3 on >password3 ~* +@all",
			},
			expectedDeleted: []string{"acl3"},
			expectedUpdated: []string{"acl2"},
			aclFile:         true,
			loadAclError:    fmt.Errorf("error loading ACL file"),
			wantErr:         true,
			expectedErrMsg:  "SyncAcls: error loading ACLs from aclFile",
		},
		{
			name: "No ACLs to update",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			expectedDeleted: []string{},
			expectedUpdated: []string{},
			aclFile:         false,
			wantErr:         false,
		},
		{
			name:           "No primary found",
			wantErr:        true,
			expectedErrMsg: "no primary found",
			aclFile:        false,
		},
		{
			name: "Error: element in sourceAclList not string",
			sourceAcls: []interface{}{
				12345, // Invalid element
			},
			destinationAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			wantErr:        true,
			expectedErrMsg: "unexpected type for ACL: int",
			aclFile:        false,
		},
		{
			name: "Error: element in destinationAclList not string",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			destinationAcls: []interface{}{
				12345, // Invalid element
			},
			wantErr:        true,
			expectedErrMsg: "unexpected type for ACL: int",
			aclFile:        false,
		},
		{
			name: "Invalid ACL in sourceAcls",
			sourceAcls: []interface{}{
				"invalid_acl",                      // Invalid ACL string
				"user acl1 on >password1 ~* +@all", // Valid ACL
			},
			destinationAcls: []interface{}{
				"user acl2 on >password2 ~* +@all",
			},
			expectedDeleted: []string{"acl2"},
			expectedUpdated: []string{"acl1"},
			aclFile:         false,
			wantErr:         false,
		},
		{
			name: "Invalid ACL in destinationAcls",
			sourceAcls: []interface{}{
				"user acl1 on >password1 ~* +@all",
			},
			destinationAcls: []interface{}{
				"invalid_acl", // Invalid ACL string
				"user acl2 on >password2 ~* +@all",
			},
			expectedDeleted: []string{"acl2"}, // acl2 should be deleted
			expectedUpdated: []string{"acl1"},
			aclFile:         false,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			primaryClient, sourceMock := redismock.NewClientMock()
			followerClient, destMock := redismock.NewClientMock()

			aclManagerPrimary := &AclManager{
				RedisClient: primaryClient,
				nodes:       make(map[string]int),
				aclFile:     tt.aclFile,
			}
			aclManagerFollower := &AclManager{
				RedisClient: followerClient,
				nodes:       make(map[string]int),
				aclFile:     tt.aclFile,
			}

			// Handle "No primary found" separately
			if tt.name == "No primary found" {
				// No ACL LIST commands should be called
				updated, deleted, err := aclManagerFollower.SyncAcls(context.Background(), nil)
				assert.Error(t, err)
				assert.Nil(t, updated)
				assert.Nil(t, deleted)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
				return
			}

			if tt.name == "Error: element in sourceAclList not string" {
				// Set up source ACL LIST expectation
				expectACLList(sourceMock, tt.sourceAcls, tt.sourceListAclsErr)

				// Run SyncAcls and assert the expected error
				updated, deleted, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
				assert.Error(t, err)
				assert.Nil(t, updated)
				assert.Nil(t, deleted)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}

				// Ensure all expectations were met
				assert.NoError(t, sourceMock.ExpectationsWereMet())
				return
			}

			// Setup source ACL LIST expectation
			expectACLList(sourceMock, tt.sourceAcls, tt.sourceListAclsErr)

			// Setup destination ACL LIST expectation only if no source ACL error
			if tt.sourceListAclsErr == nil {
				expectACLList(destMock, tt.destinationAcls, tt.destListAclsErr)
			}

			// If there is an error during ACL listing, we expect SyncAcls to return early
			if tt.sourceListAclsErr != nil || tt.destListAclsErr != nil {
				// Run SyncAcls
				updated, deleted, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
				if tt.wantErr {
					assert.Error(t, err)
					assert.Nil(t, updated)
					assert.Nil(t, deleted)
					if tt.expectedErrMsg != "" {
						assert.Contains(t, err.Error(), tt.expectedErrMsg)
					}
				} else {
					assert.NoError(t, err)
					assert.ElementsMatch(t, tt.expectedUpdated, updated)
					assert.ElementsMatch(t, tt.expectedDeleted, deleted)
				}

				// Ensure all expectations were met
				assert.NoError(t, sourceMock.ExpectationsWereMet())
				assert.NoError(t, destMock.ExpectationsWereMet())
				return
			}

			// Setup expected ACL DELUSER and ACL SETUSER commands
			for _, username := range tt.expectedDeleted {
				expectACLDelUser(destMock, username, tt.redisDoError)
			}

			for _, username := range tt.expectedUpdated {
				// Find the ACL string for the username
				var aclStr string
				for _, acl := range tt.sourceAcls.([]interface{}) {
					aclString, ok := acl.(string)
					if !ok {
						continue
					}
					if strings.Contains(aclString, "user "+username+" ") {
						aclStr = aclString
						break
					}
				}

				if aclStr == "" {
					t.Fatalf("No ACL string found for user '%s'", username)
				}

				expectACLSetUser(destMock, aclStr, tt.redisDoError)
			}

			// Setup ACL SAVE and LOAD on destMock if aclFile is enabled
			if tt.aclFile {
				if tt.saveAclError != nil {
					expectACLSave(destMock, tt.saveAclError)
				} else {
					expectACLSave(destMock, nil)
					if tt.loadAclError != nil {
						expectACLLoad(destMock, tt.loadAclError)
					} else {
						expectACLLoad(destMock, nil)
					}
				}
			}

			// Run SyncAcls
			updated, deleted, err := aclManagerFollower.SyncAcls(context.Background(), aclManagerPrimary)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedUpdated, updated)
				assert.ElementsMatch(t, tt.expectedDeleted, deleted)
			}

			// Ensure all expectations were met
			assert.NoError(t, sourceMock.ExpectationsWereMet())
			assert.NoError(t, destMock.ExpectationsWereMet())
		})
	}
}

// expectACLList sets up the expectation for the ACL LIST command.
// If err is not nil, it simulates an error; otherwise, it returns the provided ACL list.
func expectACLList(mock redismock.ClientMock, acls interface{}, err error) {
	if err != nil {
		mock.ExpectDo("ACL", "LIST").SetErr(err)
	} else {
		if acls == nil {
			acls = []interface{}{}
		}
		mock.ExpectDo("ACL", "LIST").SetVal(acls)
	}
}

// expectACLDelUser sets up the expectation for the ACL DELUSER command.
// If err is not nil, it simulates an error; otherwise, it returns "OK".
func expectACLDelUser(mock redismock.ClientMock, username string, err error) {
	if err != nil {
		mock.ExpectDo("ACL", "DELUSER", username).SetErr(err)
	} else {
		mock.ExpectDo("ACL", "DELUSER", username).SetVal("OK")
	}
}

// expectACLSetUser sets up the expectation for the ACL SETUSER command.
// If err is not nil, it simulates an error; otherwise, it returns "OK".
func expectACLSetUser(mock redismock.ClientMock, aclStr string, err error) {
	fields := strings.Fields(aclStr)
	args := []interface{}{"ACL", "SETUSER"}
	for _, field := range fields[1:] { // Skip the "user" keyword
		args = append(args, field)
	}

	if err != nil {
		mock.ExpectDo(args...).SetErr(err)
	} else {
		mock.ExpectDo(args...).SetVal("OK")
	}
}

// expectACLSave sets up the expectation for the ACL SAVE command.
// If err is not nil, it simulates an error; otherwise, it returns "OK".
func expectACLSave(mock redismock.ClientMock, err error) {
	if err != nil {
		mock.ExpectDo("ACL", "SAVE").SetErr(err)
	} else {
		mock.ExpectDo("ACL", "SAVE").SetVal("OK")
	}
}

// expectACLLoad sets up the expectation for the ACL LOAD command.
// If err is not nil, it simulates an error; otherwise, it returns "OK".
func expectACLLoad(mock redismock.ClientMock, err error) {
	if err != nil {
		mock.ExpectDo("ACL", "LOAD").SetErr(err)
	} else {
		mock.ExpectDo("ACL", "LOAD").SetVal("OK")
	}
}

func TestCurrentFunction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		mockRoleResp interface{}
		expectedFunc int
		wantErr      bool
	}{
		{
			name:         "Primary node",
			mockRoleResp: primaryRoleOutput,
			expectedFunc: Primary,
			wantErr:      false,
		},
		{
			name:         "Follower node",
			mockRoleResp: followerRoleOutput,
			expectedFunc: Follower,
			wantErr:      false,
		},
		{
			name:         "Error on ROLE command",
			mockRoleResp: nil,
			wantErr:      true,
			expectedFunc: Unknown,
		},
		{
			name: "Unknown role type",
			mockRoleResp: []interface{}{
				"sentinel",
			},
			wantErr:      true,
			expectedFunc: Unknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			if tt.wantErr {
				mock.ExpectDo("ROLE").SetErr(fmt.Errorf("error"))
			} else {
				mock.ExpectDo("ROLE").SetVal(tt.mockRoleResp)
			}

			aclManager := AclManager{
				RedisClient: redisClient,
				nodes:       make(map[string]int),
				mu:          sync.Mutex{},
			}
			ctx := context.Background()

			function, err := aclManager.CurrentFunction(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("CurrentFunction() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			assert.Equal(t, tt.expectedFunc, function)
		})
	}
}

func TestPrimary(t *testing.T) {
	tests := []struct {
		name         string
		mockRoleResp interface{}
		expectedAddr string
		wantErr      bool
	}{
		{
			name:         "Primary node returns nil",
			mockRoleResp: primaryRoleOutput,
			expectedAddr: "",
			wantErr:      false,
		},
		{
			name:         "Follower node returns primary address",
			mockRoleResp: followerRoleOutput,
			expectedAddr: "172.21.0.2:6379",
			wantErr:      false,
		},
		{
			name:         "Error on ROLE command",
			mockRoleResp: nil,
			wantErr:      true,
			expectedAddr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			if tt.wantErr {
				mock.ExpectDo("ROLE").SetErr(fmt.Errorf("error"))
			} else {
				mock.ExpectDo("ROLE").SetVal(tt.mockRoleResp)
			}

			aclManager := AclManager{
				RedisClient: redisClient,
				nodes:       make(map[string]int),
				mu:          sync.Mutex{},
			}
			ctx := context.Background()

			primary, err := aclManager.Primary(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Primary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.expectedAddr == "" {
				assert.Nil(t, primary)
			} else {
				assert.NotNil(t, primary)
				assert.Equal(t, tt.expectedAddr, primary.Addr)
			}
		})
	}
}

func TestLoop(t *testing.T) {
	t.Parallel()
	redisClient, mock := redismock.NewClientMock()
	aclManager := &AclManager{
		RedisClient: redisClient,
		nodes:       make(map[string]int),
		mu:          sync.Mutex{},
	}

	// Mock the ROLE command to return follower output
	mock.ExpectDo("ROLE").SetVal(followerRoleOutput)
	mock.ExpectDo("ROLE").SetVal(followerRoleOutput)
	// Mock listing ACLs
	mock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user default on nopass ~* &* +@all",
	})

	// Simulate SyncAcls
	mock.ExpectDo("ROLE").SetVal(followerRoleOutput)
	mock.ExpectDo("ROLE").SetVal(primaryRoleOutput)
	mock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user default on nopass ~* &* +@all",
	})
	mock.ExpectDo("ACL", "LIST").SetVal([]interface{}{
		"user default on nopass ~* &* +@all",
	})

	// Set up a cancellable context to control the loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run Loop in a separate goroutine
	go func() {
		err := aclManager.Loop(ctx, 2*time.Second)
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Errorf("Loop() error = %v", err)
		}
	}()

	// Let it run for a short time
	time.Sleep(5 * time.Second)
	cancel()
}

func TestClose(t *testing.T) {
	redisClient, _ := redismock.NewClientMock()
	aclManager := AclManager{RedisClient: redisClient}
	err := aclManager.Close()
	assert.NoError(t, err)
}

func TestClose_NilClient(t *testing.T) {
	aclManager := AclManager{RedisClient: nil}
	err := aclManager.Close()
	assert.Error(t, err)
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
		})
	}
}

func TestLoadAclFile(t *testing.T) {
	t.Parallel()
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
		})
	}
}

func TestHashString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "non-empty string",
			input:    "hello world",
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hashString(tt.input)
			assert.Equal(t, tt.expected, hash)
		})
	}
}

func TestSetBatchSize(t *testing.T) {
	tests := []struct {
		name         string
		batchSize    int
		expectedSize int
	}{
		{
			name:         "default batch size",
			batchSize:    0,
			expectedSize: 0,
		},
		{
			name:         "custom batch size",
			batchSize:    10,
			expectedSize: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aclManager := &AclManager{}
			aclManager.SetBatchSize(tt.batchSize)
			assert.Equal(t, tt.expectedSize, aclManager.batchSize)
		})
	}
}
