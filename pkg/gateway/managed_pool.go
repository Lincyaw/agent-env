package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrPoolAtCapacity is returned when admission control cannot place a session.
var ErrPoolAtCapacity = errors.New("pool at maximum capacity")

// provisioningWaitFailure reports whether a session create failed only
// because the caller stopped waiting for warm capacity (admission timeout,
// client-set allocation deadline, or client disconnect). Demand is still
// real in these cases — the client is expected to retry — so the pool must
// keep warming instead of being torn down.
func provisioningWaitFailure(err error) bool {
	return errors.Is(err, ErrPoolAtCapacity) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

func managedPoolName(
	image string,
	namespace string,
	profile string,
	privateContainers []PrivateContainerSpec,
) (string, error) {
	image = strings.TrimSpace(image)
	if image != "" {
		image = normalizeImage(image)
	}
	identity := strings.TrimSpace(namespace) + "/" + normalizeProfile(profile) + "/" + image
	if len(privateContainers) > 0 {
		raw, err := json.Marshal(privateContainers)
		if err != nil {
			return "", fmt.Errorf("marshal privateContainers identity: %w", err)
		}
		identity += "/privateContainers=" + string(raw)
	}
	h := sha256.Sum256([]byte(identity))
	hash := hex.EncodeToString(h[:6])
	slug := managedPoolImageSlug(image)
	const (
		separator     = "-"
		maxLabelBytes = 63
	)
	maxSlugBytes := maxLabelBytes - len(separator) - len(hash)
	if len(slug) > maxSlugBytes {
		slug = strings.Trim(slug[:maxSlugBytes], "-")
	}
	if slug == "" {
		slug = "image"
	}
	return slug + separator + hash, nil
}

func managedPoolImageSlug(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return "image"
	}
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at] + "-digest"
	}
	if slash := strings.LastIndex(image, "/"); slash >= 0 {
		image = image[slash+1:]
	}
	slug := strings.ToLower(image)
	slug = dnsLabelCleaner.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "image"
	}
	return slug
}

func sessionName(image, suffix string) string {
	slug := managedPoolImageSlug(image)
	const maxLabelBytes = 63
	maxSlug := maxLabelBytes - 1 - len(suffix)
	if len(slug) > maxSlug {
		slug = strings.Trim(slug[:maxSlug], "-")
	}
	return slug + "-" + suffix
}

func normalizeImage(image string) string {
	image = strings.TrimPrefix(image, "docker.io/library/")
	image = strings.TrimPrefix(image, "docker.io/")
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		image += ":latest"
	}
	return image
}
