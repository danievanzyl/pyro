package imageops

import (
	"errors"
	"fmt"
)

// SizeOverheadFactor accounts for ext4 metadata + auto-sizer headroom.
// Σ(layer_compressed_size) × this factor ≈ on-disk rootfs size.
//
// Factor of 1.3 matches the rootfs auto-sizer (commit 783cffe) so the
// pre-pull check and the actual ext4 build agree.
const sizeOverheadNumerator = 13
const sizeOverheadDenominator = 10

// ErrImageTooLarge is the sentinel for size-cap rejections. Callers use
// errors.Is to detect; errors.As pulls the typed *ImageTooLargeError for
// limit / estimated values.
var ErrImageTooLarge = errors.New("image too large")

// ImageTooLargeError carries the limit and estimated size so the API
// handler can surface them in the 413 body.
type ImageTooLargeError struct {
	LimitMB     int
	EstimatedMB int
}

func (e *ImageTooLargeError) Error() string {
	return fmt.Sprintf("image too large: estimated %d MB exceeds limit %d MB", e.EstimatedMB, e.LimitMB)
}

func (e *ImageTooLargeError) Is(target error) bool {
	return target == ErrImageTooLarge
}

// CheckSizeBudget validates Σ(layerSizes) × overhead ≤ capMB.
//
// capMB ≤ 0 is treated as unlimited (the manager applies its default
// before calling — zero here means the operator opted out).
//
// Empty/nil layer slice is rejected as malformed (a manifest with no
// layers produces an unbootable rootfs).
//
// Negative layer sizes are rejected as malformed (registry corruption).
func CheckSizeBudget(layerSizes []int64, capMB int) error {
	if len(layerSizes) == 0 {
		return errors.New("manifest has no layers")
	}
	var totalBytes int64
	for _, sz := range layerSizes {
		if sz < 0 {
			return fmt.Errorf("invalid layer size: %d", sz)
		}
		totalBytes += sz
	}
	if capMB <= 0 {
		return nil
	}
	estimatedMB := int(totalBytes / (1 << 20) * sizeOverheadNumerator / sizeOverheadDenominator)
	if estimatedMB > capMB {
		return &ImageTooLargeError{LimitMB: capMB, EstimatedMB: estimatedMB}
	}
	return nil
}
