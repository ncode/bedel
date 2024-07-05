package aclmanager

import (
	"bufio"
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

const (
	Primary = iota
	Follower
	Unknown
)

var (
	followerRegex    = regexp.MustCompile(`slave\d+:ip=(?P<ip>.+),port=(?P<port>\d+)`)
	primaryHostRegex = regexp.MustCompile(`master_host:(?P<host>.+)`)
	primaryPortRegex = regexp.MustCompile(`master_port:(?P<port>\d+)`)
	role             = regexp.MustCompile(`role:master`)
	filterUser       = regexp.MustCompile(`^user\s+`)
)

// AclManager contains the struct for managing the state of ACLs
type AclManager struct {
	Addr        string
	Username    string
	Password    string
	RedisClient *redis.Client
	primary     atomic.Bool
	nodes       map[string]int
	aclFile     bool
}

// New creates a new AclManager
func New(addr string, username string, password string, aclfile bool) *AclManager {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: username,
		Password: password,
	})
	slog.Info("Created new AclManager", "addr", addr, "username", username)
	return &AclManager{
		Addr:        addr,
		Username:    username,
		Password:    password,
		RedisClient: redisClient,
		nodes:       make(map[string]int),
		aclFile:     aclfile,
	}
}

// findNodes returns a list of nodes in the cluster based on the redis info replication command
func (a *AclManager) findNodes(ctx context.Context) error {
	slog.Debug("Entering findNodes")
	defer slog.Debug("Exiting findNodes")

	replicationInfo, err := a.RedisClient.Info(ctx, "replication").Result()
	if err != nil {
		slog.Error("Failed to get replication info", "error", err)
		return fmt.Errorf("findNodes: failed to get replication info: %w", err)
	}

	a.primary.Store(role.MatchString(replicationInfo))

	var masterHost, masterPort string
	nodes := make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(replicationInfo))
	for scanner.Scan() {
		line := scanner.Text()

		slog.Debug("Parsing line for masterHost", "line", line)
		if matches := primaryHostRegex.FindStringSubmatch(line); matches != nil {
			masterHost = matches[1]
		}

		slog.Debug("Parsing line for masterPort", "line", line)
		if matches := primaryPortRegex.FindStringSubmatch(line); matches != nil {
			masterPort = matches[1]
			node := fmt.Sprintf("%s:%s", masterHost, masterPort)
			nodes = append(nodes, node)
			a.nodes[node] = Primary
		}

		slog.Debug("Parsing line for follower", "line", line)
		if matches := followerRegex.FindStringSubmatch(line); matches != nil {
			ip := matches[followerRegex.SubexpIndex("ip")]
			port := matches[followerRegex.SubexpIndex("port")]
			node := fmt.Sprintf("%s:%s", ip, port)
			nodes = append(nodes, node)
			a.nodes[node] = Follower
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("Scanner error", "error", err)
		return fmt.Errorf("findNodes: scanner error: %w", err)
	}

	for _, node := range nodes {
		if _, ok := a.nodes[node]; !ok {
			delete(a.nodes, node)
			slog.Debug("Deleted node", "node", node)
		}
	}

	return nil
}

// CurrentFunction checks if the current node is the Primary node
func (a *AclManager) CurrentFunction(ctx context.Context) (int, error) {
	slog.Debug("Entering CurrentFunction")
	defer slog.Debug("Exiting CurrentFunction")

	err := a.findNodes(ctx)
	if err != nil {
		slog.Error("Failed to find nodes", "error", err)
		return Unknown, fmt.Errorf("CurrentFunction: %w", err)
	}
	if a.primary.Load() {
		slog.Info("Current node is Primary")
		return Primary, nil
	}
	slog.Info("Current node is Follower")
	return Follower, nil
}

func (a *AclManager) Primary(ctx context.Context) (*AclManager, error) {
	slog.Debug("Entering Primary")
	defer slog.Debug("Exiting Primary")

	err := a.findNodes(ctx)
	if err != nil {
		slog.Error("Failed to find nodes", "error", err)
		return nil, fmt.Errorf("Primary: %w", err)
	}

	for address, function := range a.nodes {
		if function == Primary {
			slog.Info("Found Primary node", "address", address)
			return New(address, a.Username, a.Password, a.aclFile), nil
		}
	}
	slog.Warn("Primary node not found")
	return nil, nil
}

// Close closes the redis client
func (a *AclManager) Close() error {
	slog.Debug("Closing Redis client")
	return a.RedisClient.Close()
}

// listAcls returns a list of acls in the cluster based on the redis acl list command
func listAcls(ctx context.Context, client *redis.Client) ([]string, error) {
	slog.Debug("Entering listAcls")
	defer slog.Debug("Exiting listAcls")

	result, err := client.Do(ctx, "ACL", "LIST").Result()
	if err != nil {
		slog.Error("Failed to list ACLs", "error", err)
		return nil, fmt.Errorf("listAcls: %w", err)
	}

	aclList, ok := result.([]interface{})
	if !ok {
		err := fmt.Errorf("unexpected result format: %v", result)
		slog.Error("Unexpected result format", "result", result)
		return nil, fmt.Errorf("listAcls: %w", err)
	}

	if len(aclList) == 0 {
		slog.Info("No ACLs found")
		return nil, nil // Return nil if no ACLs are found
	}

	acls := make([]string, len(aclList))
	for i, acl := range aclList {
		aclStr, ok := acl.(string)
		if !ok {
			err := fmt.Errorf("unexpected type for ACL: %v", acl)
			slog.Error("Unexpected type for ACL", "acl", acl)
			return nil, fmt.Errorf("listAcls: %w", err)
		}
		acls[i] = aclStr
	}
	slog.Info("Listed ACLs", "count", len(acls))
	return acls, nil
}

// saveAclFile calls the redis command ACL SAVE to save the acls to the aclFile
func saveAclFile(ctx context.Context, client *redis.Client) error {
	slog.Debug("Entering saveAclFile")
	defer slog.Debug("Exiting saveAclFile")

	if err := client.Do(ctx, "ACL", "SAVE").Err(); err != nil {
		slog.Error("Failed to save ACLs to aclFile", "error", err)
		return fmt.Errorf("saveAclFile: %w", err)
	}
	slog.Info("Saved ACLs to aclFile")
	return nil
}

// loadAclFile calls the redis command ACL LOAD to load the acls from the aclFile
func loadAclFile(ctx context.Context, client *redis.Client) error {
	slog.Debug("Entering loadAclFile")
	defer slog.Debug("Exiting loadAclFile")

	if err := client.Do(ctx, "ACL", "LOAD").Err(); err != nil {
		slog.Error("Failed to load ACLs from aclFile", "error", err)
		return fmt.Errorf("loadAclFile: %w", err)
	}
	slog.Info("Loaded ACLs from aclFile")
	return nil
}

// SyncAcls connects to master node and syncs the acls to the current node
func (a *AclManager) SyncAcls(ctx context.Context, primary *AclManager) ([]string, []string, error) {
	slog.Debug("Entering SyncAcls")
	defer slog.Debug("Exiting SyncAcls")

	if primary == nil {
		err := fmt.Errorf("no primary found")
		slog.Error("No primary found", "error", err)
		return nil, nil, err
	}

	sourceAcls, err := listAcls(ctx, primary.RedisClient)
	if err != nil {
		slog.Error("Failed to list source ACLs", "error", err)
		return nil, nil, fmt.Errorf("SyncAcls: error listing source acls: %w", err)
	}

	if a.aclFile {
		if err = saveAclFile(ctx, primary.RedisClient); err != nil {
			slog.Error("Failed to save primary ACLs to aclFile", "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error saving primary acls to aclFile: %w", err)
		}
	}

	destinationAcls, err := listAcls(ctx, a.RedisClient)
	if err != nil {
		slog.Error("Failed to list current ACLs", "error", err)
		return nil, nil, fmt.Errorf("SyncAcls: error listing current acls: %w", err)
	}

	toUpdate := make(map[string]string)
	for _, acl := range sourceAcls {
		username := strings.Fields(acl)[1]
		toUpdate[username] = acl
	}

	var updated, deleted []string

	for _, acl := range destinationAcls {
		username := strings.Fields(acl)[1]
		if currentAcl, found := toUpdate[username]; found {
			if currentAcl == acl {
				delete(toUpdate, username)
				slog.Debug("ACL already in sync", "username", username)
			}
			continue
		}

		slog.Debug("Deleting ACL", "username", username)
		if err := a.RedisClient.Do(ctx, "ACL", "DELUSER", username).Err(); err != nil {
			slog.Error("Failed to delete ACL", "username", username, "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error deleting acl: %w", err)
		}
		deleted = append(deleted, username)
	}

	for username, acl := range toUpdate {
		slog.Debug("Syncing ACL", "username", username, "line", acl)
		command := strings.Split(filterUser.ReplaceAllString(acl, "ACL SETUSER "), " ")
		commandInterface := make([]interface{}, len(command))
		for i, s := range command {
			commandInterface[i] = s
		}
		if err := a.RedisClient.Do(ctx, commandInterface...).Err(); err != nil {
			slog.Error("Failed to set ACL", "username", username, "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error setting acl: %w", err)
		}
		updated = append(updated, username)
	}

	if a.aclFile {
		if err = saveAclFile(ctx, a.RedisClient); err != nil {
			slog.Error("Failed to save ACLs to aclFile", "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error saving acls to aclFile: %w", err)
		}
		if err = loadAclFile(ctx, a.RedisClient); err != nil {
			slog.Error("Failed to load synced ACLs from aclFile", "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error loading synced acls from aclFile: %w", err)
		}
	}

	slog.Info("Synced ACLs", "added", updated, "deleted", deleted)
	return updated, deleted, nil
}

// Loop loops through the sync interval and syncs the acls
func (a *AclManager) Loop(ctx context.Context) error {
	slog.Debug("Entering Loop")
	defer slog.Debug("Exiting Loop")

	ticker := time.NewTicker(viper.GetDuration("syncInterval") * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Context done, exiting Loop")
			return nil
		case <-ticker.C:
			function, err := a.CurrentFunction(ctx)
			if err != nil {
				slog.Warn("Unable to check if it's a Primary", "error", err)
				continue
			}
			if function == Follower {
				primary, err := a.Primary(ctx)
				if err != nil {
					slog.Warn("Unable to find Primary", "error", err)
					continue
				}
				added, deleted, err := a.SyncAcls(ctx, primary)
				if err != nil {
					slog.Warn("Unable to sync ACLs from Primary", "error", err)
					continue
				}
				slog.Info("Synced ACLs from Primary", "added", added, "deleted", deleted)
			}
		}
	}
}
