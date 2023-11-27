package aclmanager

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-redis/redismock/v9"
	"github.com/stretchr/testify/assert"
)

var (
	masterOutput = `
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

	slaveOutput = `
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
			mockResp: masterOutput,
			want: []NodeInfo{
				{
					Address:  "172.21.0.3:6379",
					Function: "slave",
				},
			},
			wantErr: false,
		},
		{
			name:     "parse slave output",
			mockResp: slaveOutput,
			want: []NodeInfo{
				{
					Address:  "172.21.0.2:6379",
					Function: "master",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redisClient, mock := redismock.NewClientMock()

			// Mocking the response for the Info function
			mock.ExpectInfo("replication").SetVal(tt.mockResp)
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
			want: []string{
				"user default on nopass ~* &* +@all",
			},
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
			sourceAcls:      []interface{}{"acl1", "acl2"},
			destinationAcls: []interface{}{"acl1", "acl3"},
			expectedDeleted: []string{"acl3"},
			expectedAdded:   []string{"acl2"},
			wantErr:         false,
		},
		{
			name:            "No ACLs to delete",
			sourceAcls:      []interface{}{"acl1", "acl2"},
			destinationAcls: []interface{}{"acl1", "acl2"},
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
						destMock.ExpectDo("ACL", "DELUSER", acl).SetVal("OK")
					}
				}
				if tt.expectedAdded != nil {
					for _, acl := range tt.expectedAdded {
						destMock.ExpectDo("ACL", "SETUSER", acl).SetVal("OK")
					}
				}
			}

			deleted, err := mirrorAcls(context.Background(), sourceClient, destinationClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("mirrorAcls() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(deleted, tt.expectedDeleted) {
				t.Errorf("mirrorAcls() deleted = %v, expectedDeleted %v", deleted, tt.expectedDeleted)
			}
		})
	}
}

func BenchmarkParseRedisOutputSlave(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := parseRedisOutput(slaveOutput)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseRedisOutputMaster(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := parseRedisOutput(masterOutput)
		if err != nil {
			b.Fatal(err)
		}
	}
}
