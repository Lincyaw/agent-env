package scheduler

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
)

// ComputeTopK returns the top-k node names for the given image key using
// Highest Random Weight (Rendezvous) hashing. The result is deterministic
// for the same (image, nodes, k) inputs. Uses only Go stdlib (crypto/sha256).
//
// If k >= len(nodes), all nodes are returned sorted by HRW score.
func ComputeTopK(image string, nodes []string, k int) []string {
	if len(nodes) == 0 || k <= 0 {
		return nil
	}
	if k > len(nodes) {
		k = len(nodes)
	}

	type scored struct {
		name  string
		score uint64
	}

	scored_nodes := make([]scored, len(nodes))
	for i, node := range nodes {
		scored_nodes[i] = scored{
			name:  node,
			score: hrwScore(image, node),
		}
	}

	sort.Slice(scored_nodes, func(i, j int) bool {
		if scored_nodes[i].score != scored_nodes[j].score {
			return scored_nodes[i].score > scored_nodes[j].score
		}
		return scored_nodes[i].name < scored_nodes[j].name
	})

	result := make([]string, k)
	for i := 0; i < k; i++ {
		result[i] = scored_nodes[i].name
	}
	return result
}

// hrwScore computes a 64-bit hash of (image, node) for HRW ranking.
func hrwScore(image, node string) uint64 {
	h := sha256.New()
	h.Write([]byte(image))
	h.Write([]byte{0}) // separator
	h.Write([]byte(node))
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}
