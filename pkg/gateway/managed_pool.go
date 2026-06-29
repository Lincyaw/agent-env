package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrPoolAtCapacity is returned when admission control cannot place a session.
var ErrPoolAtCapacity = errors.New("pool at maximum capacity")

func managedPoolName(image, namespace, profile string, configEnv json.RawMessage) (string, error) {
	identity := namespace + "/" + normalizeProfile(profile) + "/" + image
	cfg, err := decodeConfigEnv(configEnv)
	if err != nil {
		return "", err
	}
	if cfg != nil {
		raw, err := json.Marshal(cfg)
		if err != nil {
			return "", fmt.Errorf("marshal configEnv identity: %w", err)
		}
		identity += "/" + string(raw)
	}
	h := sha256.Sum256([]byte(identity))
	return "managed-" + hex.EncodeToString(h[:6]), nil
}

func normalizeImage(image string) string {
	image = strings.TrimPrefix(image, "docker.io/library/")
	image = strings.TrimPrefix(image, "docker.io/")
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		image += ":latest"
	}
	return image
}
