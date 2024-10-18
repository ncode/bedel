package aclmanager

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Constants for node roles
const (
	Primary = iota
	Follower
	Unknown
)

// AclManager contains the struct for managing the state of ACLs
type AclManager struct {
	Addr        string
	Username    string
	Password    string
	RedisClient *redis.Client
	primary     atomic.Bool
	nodes       map[string]int
	mu          sync.Mutex // Mutex to protect nodes map
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

// findNodes returns a list of nodes in the cluster based on the redis ROLE command
func (a *AclManager) findNodes(ctx context.Context) error {
	slog.Debug("Entering findNodes")
	defer slog.Debug("Exiting findNodes")

	roleInfo, err := a.RedisClient.Do(ctx, "ROLE").Result()
	if err != nil {
		slog.Error("Failed to get role info", "error", err)
		return fmt.Errorf("findNodes: failed to get role info: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	// Clear the nodes map
	a.nodes = make(map[string]int)

	switch info := roleInfo.(type) {
	case []interface{}:
		if len(info) == 0 {
			slog.Error("ROLE command returned empty result")
			return fmt.Errorf("findNodes: ROLE command returned empty result")
		}

		roleType, ok := info[0].(string)
		if !ok {
			slog.Error("Unexpected type for role", "type", fmt.Sprintf("%T", info[0]))
			return fmt.Errorf("findNodes: unexpected type for role: %T", info[0])
		}

		switch roleType {
		case "master":
			a.primary.Store(true)
			// Parse connected slaves
			if len(info) >= 3 {
				slaves, ok := info[2].([]interface{})
				if ok {
					for _, slaveInfo := range slaves {
						if slaveArr, ok := slaveInfo.([]interface{}); ok && len(slaveArr) >= 2 {
							ip, _ := slaveArr[0].(string)
							port, _ := slaveArr[1].(int64)
							node := fmt.Sprintf("%s:%d", ip, port)
							a.nodes[node] = Follower
						}
					}
				}
			}
		case "slave":
			a.primary.Store(false)
			// Get master info
			if len(info) >= 3 {
				masterHost, _ := info[1].(string)
				masterPort, _ := info[2].(int64)
				node := fmt.Sprintf("%s:%d", masterHost, masterPort)
				a.nodes[node] = Primary
			}
		default:
			slog.Error("Unknown role type", "role", roleType)
			return fmt.Errorf("findNodes: unknown role type: %s", roleType)
		}
	default:
		slog.Error("Unexpected type for roleInfo", "type", fmt.Sprintf("%T", roleInfo))
		return fmt.Errorf("findNodes: unexpected type for roleInfo: %T", roleInfo)
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

// Primary returns an AclManager connected to the primary node
func (a *AclManager) Primary(ctx context.Context) (*AclManager, error) {
	slog.Debug("Entering Primary")
	defer slog.Debug("Exiting Primary")

	err := a.findNodes(ctx)
	if err != nil {
		slog.Error("Failed to find nodes", "error", err)
		return nil, fmt.Errorf("Primary: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
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
	if a.RedisClient == nil {
		return fmt.Errorf("Redis client is nil")
	}
	return a.RedisClient.Close()
}

// listAcls returns a list of ACLs in the cluster based on the redis ACL LIST command
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
		err := fmt.Errorf("unexpected result format: %T", result)
		slog.Error("Unexpected result format", "result", result)
		return nil, fmt.Errorf("listAcls: %w", err)
	}

	acls := make([]string, 0, len(aclList))
	for _, acl := range aclList {
		aclStr, ok := acl.(string)
		if !ok {
			err := fmt.Errorf("unexpected type for ACL: %T", acl)
			slog.Error("Unexpected type for ACL", "acl", acl)
			return nil, fmt.Errorf("listAcls: %w", err)
		}
		acls = append(acls, aclStr)
	}
	slog.Info("Listed ACLs", "count", len(acls))
	return acls, nil
}

// saveAclFile calls the redis command ACL SAVE to save the ACLs to the aclFile
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

// loadAclFile calls the redis command ACL LOAD to load the ACLs from the aclFile
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

// hashString computes a SHA-256 hash of the input string
func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// listAndMapAcls is an auxiliary function to list ACLs and create a map of username to hash and ACL string
func listAndMapAcls(ctx context.Context, client *redis.Client) (map[string]string, map[string]string, error) {
	slog.Debug("Entering listAndMapAcls")
	defer slog.Debug("Exiting listAndMapAcls")

	result, err := client.Do(ctx, "ACL", "LIST").Result()
	if err != nil {
		slog.Error("Failed to list ACLs", "error", err)
		return nil, nil, fmt.Errorf("listAndMapAcls: error listing ACLs: %w", err)
	}

	aclList, ok := result.([]interface{})
	if !ok {
		err := fmt.Errorf("unexpected result format: %T", result)
		slog.Error("Unexpected result format", "result", result)
		return nil, nil, fmt.Errorf("listAndMapAcls: %w", err)
	}

	aclHashMap := make(map[string]string)
	aclStrMap := make(map[string]string)
	for _, acl := range aclList {
		aclStr, ok := acl.(string)
		if !ok {
			err := fmt.Errorf("unexpected type for ACL: %T", acl)
			slog.Error("Unexpected type for ACL", "acl", acl)
			return nil, nil, fmt.Errorf("listAndMapAcls: %w", err)
		}
		fields := strings.Fields(aclStr)
		if len(fields) < 2 {
			slog.Warn("Invalid ACL format", "acl", aclStr)
			continue
		}
		username := fields[1]
		hash := hashString(aclStr)
		aclHashMap[username] = hash
		aclStrMap[username] = aclStr
	}

	slog.Info("Listed and mapped ACLs", "count", len(aclHashMap))
	return aclHashMap, aclStrMap, nil
}

// SyncAcls connects to the primary node and syncs the ACLs to the current node
func (a *AclManager) SyncAcls(ctx context.Context, primary *AclManager) ([]string, []string, error) {
	slog.Debug("Entering SyncAcls")
	defer slog.Debug("Exiting SyncAcls")

	if primary == nil {
		err := fmt.Errorf("no primary found")
		slog.Error("No primary found", "error", err)
		return nil, nil, err
	}

	const batchSize = 100

	// Get source ACLs
	sourceAclHashMap, sourceAclStrMap, err := listAndMapAcls(ctx, primary.RedisClient)
	if err != nil {
		return nil, nil, fmt.Errorf("SyncAcls: error listing source ACLs: %w", err)
	}

	// Get destination ACLs
	destinationAclHashMap, _, err := listAndMapAcls(ctx, a.RedisClient)
	if err != nil {
		return nil, nil, fmt.Errorf("SyncAcls: error listing destination ACLs: %w", err)
	}

	var updated, deleted []string

	// Batch commands
	cmds := make([]redis.Cmder, 0, batchSize)
	pipe := a.RedisClient.Pipeline()

	// Delete ACLs that are not in the source
	for username := range destinationAclHashMap {
		if _, found := sourceAclHashMap[username]; !found && username != "default" {
			slog.Debug("Deleting ACL", "username", username)
			cmd := pipe.Do(ctx, "ACL", "DELUSER", username)
			cmds = append(cmds, cmd)
			deleted = append(deleted, username)
			if len(cmds) >= batchSize {
				// Execute pipeline
				if _, err = pipe.Exec(ctx); err != nil {
					slog.Error("Failed to execute pipeline", "error", err)
					return nil, nil, fmt.Errorf("SyncAcls: error executing pipeline: %w", err)
				}
				// Reset pipeline and cmds
				pipe = a.RedisClient.Pipeline()
				cmds = cmds[:0]
			}
		}
	}

	// Add or update ACLs from the source
	for username, sourceHash := range sourceAclHashMap {
		destHash, found := destinationAclHashMap[username]
		if !found || destHash != sourceHash {
			aclStr := sourceAclStrMap[username]
			if aclStr == "" {
				slog.Error("ACL string not found for user", "username", username)
				continue
			}

			args := []interface{}{"ACL", "SETUSER"}
			fields := strings.Fields(aclStr)
			// Skip the "user" keyword
			for _, field := range fields[1:] {
				args = append(args, field)
			}

			cmd := pipe.Do(ctx, args...)
			cmds = append(cmds, cmd)
			updated = append(updated, username)

			if len(cmds) >= batchSize {
				// Execute pipeline
				if _, err = pipe.Exec(ctx); err != nil {
					slog.Error("Failed to execute pipeline", "error", err)
					return nil, nil, fmt.Errorf("SyncAcls: error executing pipeline: %w", err)
				}
				// Reset pipeline and cmds
				pipe = a.RedisClient.Pipeline()
				cmds = cmds[:0]
			}
		}
	}

	// Execute any remaining commands
	if len(cmds) > 0 {
		if _, err = pipe.Exec(ctx); err != nil {
			slog.Error("Failed to execute pipeline", "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error executing pipeline: %w", err)
		}
	}

	// If aclFile is enabled, save and load the ACL file
	if a.aclFile {
		if err = saveAclFile(ctx, a.RedisClient); err != nil {
			slog.Error("Failed to save ACLs to aclFile", "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error saving ACLs to aclFile: %w", err)
		}
		if err = loadAclFile(ctx, a.RedisClient); err != nil {
			slog.Error("Failed to load synced ACLs from aclFile", "error", err)
			return nil, nil, fmt.Errorf("SyncAcls: error loading ACLs from aclFile: %w", err)
		}
	}

	slog.Info("Synced ACLs", "updated", updated, "deleted", deleted)
	return updated, deleted, nil
}

// Loop periodically syncs the ACLs at the given interval
func (a *AclManager) Loop(ctx context.Context, syncInterval time.Duration) error {
	slog.Debug("Entering Loop")
	defer slog.Debug("Exiting Loop")

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Context done, exiting Loop")
			return ctx.Err()
		case <-ticker.C:
			function, err := a.CurrentFunction(ctx)
			if err != nil {
				slog.Warn("Unable to determine node function", "error", err)
				continue
			}
			if function == Follower {
				primary, err := a.Primary(ctx)
				if err != nil {
					slog.Warn("Unable to find Primary", "error", err)
					continue
				}
				if primary == nil {
					slog.Warn("Primary node is nil")
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
