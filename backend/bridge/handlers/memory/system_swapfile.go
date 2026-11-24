package memory

import "context"

// The swap file blocks on the host. Each is a hostRoot method, so that a test
// runs it on a fixture tree as an ordinary user.

func (s liveSystem) makeSwapFile(ctx context.Context, path string, size uint64) (fileID, error) {
	return s.root.makeSwapFile(ctx, path, size)
}

func (s liveSystem) resizeSwapFile(ctx context.Context, path string, id fileID, size uint64) error {
	return s.root.resizeSwapFile(ctx, path, id, size)
}

func (s liveSystem) deleteSwapFile(ctx context.Context, path string, id fileID) error {
	return s.root.deleteSwapFile(ctx, path, id)
}

func (s liveSystem) swapOn(ctx context.Context, path, options string) error {
	return s.root.swapOn(ctx, path, options)
}

func (s liveSystem) addFstabSwap(ctx context.Context, path string) error {
	return s.root.addFstabSwap(ctx, path)
}

func (s liveSystem) removeFstabSwap(ctx context.Context, path string) error {
	return s.root.removeFstabSwap(ctx, path)
}
