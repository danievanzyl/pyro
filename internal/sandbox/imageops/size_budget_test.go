package imageops

import (
	"errors"
	"testing"
)

func TestSizeBudget_UnderCap(t *testing.T) {
	// 100 MB layer × 1.3 = 130 MB est, cap 4096 MB → ok.
	if err := CheckSizeBudget([]int64{100 << 20}, 4096); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestSizeBudget_AtCap(t *testing.T) {
	// 1000 MB layer × 1.3 = 1300 MB est, cap 1300 MB → ok (boundary).
	if err := CheckSizeBudget([]int64{1000 << 20}, 1300); err != nil {
		t.Fatalf("at-cap should pass, got %v", err)
	}
}

func TestSizeBudget_JustOver(t *testing.T) {
	// 1000 MB × 1.3 = 1300, cap 1299 → reject.
	err := CheckSizeBudget([]int64{1000 << 20}, 1299)
	if err == nil {
		t.Fatal("expected ErrImageTooLarge, got nil")
	}
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("expected ErrImageTooLarge, got %v", err)
	}
	var tooLarge *ImageTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *ImageTooLargeError, got %T", err)
	}
	if tooLarge.LimitMB != 1299 {
		t.Errorf("limit: want 1299, got %d", tooLarge.LimitMB)
	}
	if tooLarge.EstimatedMB != 1300 {
		t.Errorf("estimated: want 1300, got %d", tooLarge.EstimatedMB)
	}
}

func TestSizeBudget_MultipleLayers(t *testing.T) {
	// 500 + 500 + 500 = 1500 MB × 1.3 = 1950 MB
	layers := []int64{500 << 20, 500 << 20, 500 << 20}
	if err := CheckSizeBudget(layers, 2000); err != nil {
		t.Errorf("under cap should pass, got %v", err)
	}
	if err := CheckSizeBudget(layers, 1949); err == nil {
		t.Errorf("over cap should fail")
	}
}

func TestSizeBudget_EmptyLayers(t *testing.T) {
	// Malformed manifest — reject.
	err := CheckSizeBudget(nil, 4096)
	if err == nil {
		t.Fatal("expected error for empty layers")
	}
	if errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("empty should not be ErrImageTooLarge, got %v", err)
	}
}

func TestSizeBudget_ZeroCap(t *testing.T) {
	// Zero cap = unlimited (caller decides — manager applies default).
	if err := CheckSizeBudget([]int64{1 << 40}, 0); err != nil {
		t.Errorf("zero cap should be unlimited, got %v", err)
	}
}

func TestSizeBudget_NegativeLayer(t *testing.T) {
	// Defensive: negative layer size is malformed.
	err := CheckSizeBudget([]int64{-1}, 4096)
	if err == nil {
		t.Fatal("expected error for negative layer size")
	}
}
