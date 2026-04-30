//go:build !linux

package imageops

import (
	"context"
	"errors"
)

// Ext4Builder is a stub on non-Linux platforms.
type Ext4Builder struct{}

// NewExt4Builder constructs a builder.
func NewExt4Builder() *Ext4Builder { return &Ext4Builder{} }

// Mounted is a stub mounted image handle.
type Mounted struct {
	ImagePath string
	MountDir  string
}

var errLinuxOnly = errors.New("ext4 operations require Linux")

func (b *Ext4Builder) Create(ctx context.Context, imagePath string, sizeMB int) (*Mounted, error) {
	return nil, errLinuxOnly
}
func (b *Ext4Builder) Open(ctx context.Context, imagePath string) (*Mounted, error) {
	return nil, errLinuxOnly
}
func (m *Mounted) Close() error { return errLinuxOnly }
