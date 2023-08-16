package redisConfig

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestParse(t *testing.T) {
	// Mock redis.conf content
	mockContent := `
# This is a mock redis.conf for testing
masterauth someAuthKey
masteruser someMasterUser
aclfile /path/to/acl/file
`

	// Create a temporary file
	tmpfile, err := ioutil.TempFile("", "mock_redis.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	// Write mock content to temp file
	_, err = tmpfile.Write([]byte(mockContent))
	if err != nil {
		tmpfile.Close()
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpfile.Close()

	// Test the Parse function
	config, err := Parse(tmpfile.Name())
	if err != nil {
		t.Fatalf("Parse function returned error: %v", err)
	}

	// Validate the parsed content
	if config.MasterAuth != "someAuthKey" {
		t.Errorf("Expected MasterAuth = someAuthKey, got = %s", config.MasterAuth)
	}
	if config.MasterUser != "someMasterUser" {
		t.Errorf("Expected MasterUser = someMasterUser, got = %s", config.MasterUser)
	}
	if config.AclFile != "/path/to/acl/file" {
		t.Errorf("Expected AclFile = /path/to/acl/file, got = %s", config.AclFile)
	}
}

func TestParseWithMissingValues(t *testing.T) {
	// Mock redis.conf content with missing masteruser
	mockContent := `
masterauth someAuthKey
aclfile /path/to/acl/file
`

	tmpfile, err := ioutil.TempFile("", "mock_redis_missing.conf")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	_, err = tmpfile.Write([]byte(mockContent))
	if err != nil {
		tmpfile.Close()
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpfile.Close()

	config, err := Parse(tmpfile.Name())
	if err == nil || err.Error() != "some fields in the config are missing or empty" {
		t.Fatalf("Expected an error about missing fields, got: %v", err)
	}
	if config.MasterUser != "" {
		t.Errorf("Expected missing MasterUser, but got: %s", config.MasterUser)
	}
}
