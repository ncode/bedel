package redisConfig

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)
import (
	"errors"
)

type RedisConfig struct {
	MasterAuth string
	MasterUser string
	AclFile    string
}

func Parse(path string) (*RedisConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	values := map[string]string{
		"masterauth": "",
		"masteruser": "",
		"aclfile":    "",
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		for key := range values {
			if strings.HasPrefix(line, key) {
				parts := strings.SplitN(line, " ", 2)
				if len(parts) == 2 {
					values[key] = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	config := &RedisConfig{
		MasterAuth: values["masterauth"],
		MasterUser: values["masteruser"],
		AclFile:    values["aclfile"],
	}

	if config.MasterAuth == "" || config.MasterUser == "" || config.AclFile == "" {
		return config, errors.New("some fields in the config are missing or empty")
	}

	return config, nil
}
