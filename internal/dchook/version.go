package dchook

import (
	"errors"
	"strconv"
	"strings"
)

const (
	versionParts = 3
	numbers      = "0123456789"
	devVersion   = "dev"
)

// ErrInvalidSemVer returns an invalid Semantic version.
var ErrInvalidSemVer = errors.New("invalid semantic version")

// Version is used to ensure that versions are compatible.
type Version struct {
	major, minor, patch uint64
	commit              string
	isDevVersion        bool
}

// ParseVersion parses the version string and commit into a Version struct.
func ParseVersion(s, commit string) (*Version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")

	if s == devVersion {
		return &Version{isDevVersion: true, commit: commit}, nil
	}

	parts := strings.SplitN(s, ".", versionParts)

	if len(parts) < versionParts {
		return nil, ErrInvalidSemVer
	}

	numParts := make([]uint64, versionParts)

	for i, p := range parts {
		if !containsOnly(p, numbers) {
			return nil, ErrInvalidSemVer
		}

		if len(p) > 1 {
			p = strings.TrimLeft(p, "0")
			if p == "" {
				p = "0"
			}
		}

		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, ErrInvalidSemVer
		}

		numParts[i] = n
	}

	return &Version{
		major:        numParts[0],
		minor:        numParts[1],
		patch:        numParts[2],
		commit:       commit,
		isDevVersion: false,
	}, nil
}

// IsCompatible compares the current version alternate version to see if they are
// compatible.
//
// - If either version is a development version, it's considered compatible.
// - If the major versions differ, it is not compatible.
// - If the minor versions differ, it is not compatible.
// - If the patch versions match, the commit versions must match.
func (v *Version) IsCompatible(o *Version) bool {
	if v.isDevVersion || o.isDevVersion {
		return true
	}

	if v.major != o.major || v.minor != o.minor {
		return false
	}

	if v.patch == o.patch && v.commit != o.commit {
		return false
	}

	return true
}

func containsOnly(s, comp string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return !strings.ContainsRune(comp, r)
	}) == -1
}
