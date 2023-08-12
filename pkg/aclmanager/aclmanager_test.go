package aclmanager

import (
	"testing"

	"github.com/go-redis/redismock/v9"
	"github.com/stretchr/testify/assert"
)

func TestFindNodes(t *testing.T) {
	// Sample master and slave output for testing
	masterOutput := `
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

	slaveOutput := `
# Replication
role:slave
master_host:aclmanager-master
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
					Host:     "172.21.0.3",
					Port:     "6379",
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
					Host:     "aclmanager-master",
					Port:     "6379",
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
