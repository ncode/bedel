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

// AclManager containers the struct for bedel to manager the state of aclmanager acls
type AclManager struct {
	Addr        string
	Username    string
	Password    string
	RedisClient *redis.Client
	primary     atomic.Bool
	nodes       map[string]int
	aclfile     bool
}

// New creates a new AclManager
func New(addr string, username string, password string, aclfile bool) *AclManager {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: username,
		Password: password,
	})
	return &AclManager{
		Addr:        addr,
		Username:    username,
		Password:    password,
		RedisClient: redisClient,
		nodes:       make(map[string]int),
		aclfile:     aclfile,
	}
}

// findNodes returns a list of nodes in the cluster based on the redis info replication command
func (a *AclManager) findNodes(ctx context.Context) (err error) {
	slog.Debug("Finding nodes")
	replicationInfo, err := a.RedisClient.Info(ctx, "replication").Result()
	if err != nil {
		return err
	}

	a.primary.Store(role.MatchString(replicationInfo))

	var masterHost, masterPort string
	var nodes []string
	scanner := bufio.NewScanner(strings.NewReader(replicationInfo))
	for scanner.Scan() {
		line := scanner.Text()

		slog.Debug("Parsing line looking for masterHost", "content", line)
		if matches := primaryHostRegex.FindStringSubmatch(line); matches != nil {
			masterHost = matches[1]
		}

		slog.Debug("Parsing line looking for Follower", "content", line)
		if matches := primaryPortRegex.FindStringSubmatch(line); matches != nil {
			masterPort = matches[1]
			nodes = append(nodes, fmt.Sprintf("%s:%s", masterHost, masterPort))
			a.nodes[fmt.Sprintf("%s:%s", masterHost, masterPort)] = Primary
		}

		if matches := followerRegex.FindStringSubmatch(line); matches != nil {
			ip := matches[followerRegex.SubexpIndex("ip")]
			port := matches[followerRegex.SubexpIndex("port")]
			nodes = append(nodes, fmt.Sprintf("%s:%s", ip, port))
			a.nodes[fmt.Sprintf("%s:%s", ip, port)] = Follower
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	for _, node := range nodes {
		if _, ok := a.nodes[node]; !ok {
			delete(a.nodes, node)
		}
	}

	return err
}

// CurrentFunction check if the current node is the Primary node
func (a *AclManager) CurrentFunction(ctx context.Context) (function int, err error) {
	slog.Debug("Check node current function")
	err = a.findNodes(ctx)
	if err != nil {
		return Unknown, err
	}
	if a.primary.Load() {
		return Primary, err
	}

	return Follower, err
}

func (a *AclManager) Primary(ctx context.Context) (primary *AclManager, err error) {
	err = a.findNodes(ctx)
	if err != nil {
		return nil, err
	}

	for address, function := range a.nodes {
		if function == Primary {
			return New(address, a.Username, a.Password, a.aclfile), err
		}
	}

	return nil, err
}

// Close closes the redis client
func (a *AclManager) Close() error {
	return a.RedisClient.Close()
}

// listAcls returns a list of acls in the cluster based on the redis acl list command
func listAcls(ctx context.Context, client *redis.Client) (acls []string, err error) {
	result, err := client.Do(ctx, "ACL", "LIST").Result()
	if err != nil {
		return nil, err
	}

	aclList, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected result format: %v", result)
	}

	if len(aclList) == 0 {
		return nil, nil // Return nil if no ACLs are found
	}

	acls = make([]string, len(aclList))
	for i, acl := range aclList {
		aclStr, ok := acl.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected type for ACL: %v", acl)
		}
		acls[i] = aclStr
	}

	return acls, nil
}

// saveAclFile call the redis command ACL SAVE to save the acls to the aclfile
func saveAclFile(ctx context.Context, client *redis.Client) error {
	slog.Debug("Saving acls to aclfile")
	if err := client.Do(ctx, "ACL", "SAVE").Err(); err != nil {
		return fmt.Errorf("error saving acls to aclfile: %v", err)
	}

	return nil
}

// loadAclFile call the redis command ACL LOAD to load the acls from the aclfile
func loadAclFile(ctx context.Context, client *redis.Client) error {
	slog.Debug("Loading acls from aclfile")
	if err := client.Do(ctx, "ACL", "LOAD").Err(); err != nil {
		return fmt.Errorf("error loading acls from aclfile: %v", err)
	}

	return nil
}

// SyncAcls connects to master node and syncs the acls to the current node
func (a *AclManager) SyncAcls(ctx context.Context, primary *AclManager) (updated []string, deleted []string, err error) {
	slog.Debug("Syncing acls")
	if primary == nil {
		return updated, deleted, fmt.Errorf("no primary found")
	}

	sourceAcls, err := listAcls(ctx, primary.RedisClient)
	if err != nil {
		return nil, nil, fmt.Errorf("error listing source acls: %v", err)
	}

	if a.aclfile {
		err = saveAclFile(ctx, primary.RedisClient)
		if err != nil {
			return nil, nil, fmt.Errorf("error saving primary acls to aclfile: %v", err)
		}
	}

	destinationAcls, err := listAcls(ctx, a.RedisClient)
	if err != nil {
		return nil, nil, fmt.Errorf("error listing current acls: %v", err)
	}

	// Map to keep track of ACLs to add
	toUpdate := make(map[string]string)
	for _, acl := range sourceAcls {
		username := strings.Split(acl, " ")[1]
		toUpdate[username] = acl
	}

	// Delete ACLs not in source and remove from the toUpdate map if present in destination
	for _, acl := range destinationAcls {
		username := strings.Split(acl, " ")[1]
		if currentAcl, found := toUpdate[username]; found {
			if currentAcl == acl {
				// If found in source, don't need to add, so remove from map
				delete(toUpdate, username)
				slog.Debug("ACL already in sync", "username", username)
			}
			continue
		}

		// If not found in source, delete from destination
		slog.Debug("Deleting ACL", "username", username)
		if err := a.RedisClient.Do(context.Background(), "ACL", "DELUSER", username).Err(); err != nil {
			return nil, nil, fmt.Errorf("error deleting acl: %v", err)
		}
		deleted = append(deleted, username)
	}

	// Add remaining ACLs from source
	for username, acl := range toUpdate {
		slog.Debug("Syncing ACL", "username", username, "line", acl)
		command := strings.Split(filterUser.ReplaceAllString(acl, "ACL SETUSER "), " ")
		commandInterfce := make([]interface{}, len(command))
		for i, s := range command {
			commandInterfce[i] = s
		}
		if err := a.RedisClient.Do(context.Background(), commandInterfce...).Err(); err != nil {
			return nil, nil, fmt.Errorf("error setting acl: %v", err)
		}
		updated = append(updated, username)
	}

	if a.aclfile {
		err = saveAclFile(ctx, a.RedisClient)
		if err != nil {
			return nil, nil, fmt.Errorf("error saving acls to aclfile: %v", err)
		}
		err = loadAclFile(ctx, primary.RedisClient)
		if err != nil {
			return nil, nil, fmt.Errorf("error loading synced acls from aclfile: %v", err)
		}
	}

	return updated, deleted, nil
}

// Loop loops through the sync interval and syncs the acls
func (a *AclManager) Loop(ctx context.Context) (err error) {
	ticker := time.NewTicker(viper.GetDuration("syncInterval") * time.Second)
	defer ticker.Stop()

	var primary *AclManager
	for {
		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
			function, e := a.CurrentFunction(ctx)
			if err != nil {
				slog.Warn("unable to check if it's a Primary", "message", err)
				err = fmt.Errorf("unable to check if it's a Primary: %w", err)
			}
			if function == Follower {
				primary, err = a.Primary(ctx)
				if err != nil {
					slog.Warn("unable to find Primary", "message", e)
					continue
				}
				var added, deleted []string
				added, deleted, err = a.SyncAcls(ctx, primary)
				if err != nil {
					slog.Warn("unable to sync acls from Primary", "message", err)
					err = fmt.Errorf("unable to sync acls from Primary: %w", err)
					continue
				}
				slog.Info("Synced acls from Primary", "added", added, "deleted", deleted)
			}
		}
	}
}
