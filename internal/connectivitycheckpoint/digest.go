package connectivitycheckpoint

import (
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityreduce"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// proposalsDigest addresses the whole proposal set, including an empty one.
//
// An empty set gets a digest rather than an empty string so that "produced no
// proposals" is an attested outcome instead of a missing field.
func proposalsDigest(proposals []connectivityreduce.Proposal) (string, error) {
	if proposals == nil {
		proposals = []connectivityreduce.Proposal{}
	}
	digest, _, err := policy.CanonicalSHA256(proposals)
	if err != nil {
		return "", ErrInvalidCheckpoint
	}
	return digest, nil
}
