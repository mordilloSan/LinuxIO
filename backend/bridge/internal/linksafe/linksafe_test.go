package linksafe

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

// linkTree is base/home with data, a link with an absolute target to
// base/disk/data, a folder holding sub. base is its real path, so the paths
// the walk names can be compared with it.
type linkTree struct {
	base, home, link, real string
}

func newLinkTree(t *testing.T) linkTree {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	tree := linkTree{base: base, home: filepath.Join(base, "home"), real: filepath.Join(base, "disk", "data")}
	require.NoError(t, os.MkdirAll(tree.home, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tree.real, "sub"), 0o755))
	tree.link = filepath.Join(tree.home, "data")
	require.NoError(t, os.Symlink(tree.real, tree.link))
	return tree
}

func openRoot(t *testing.T) *fsroot.FSRoot {
	t.Helper()
	root, err := fsroot.Open()
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// A bridge that acts as the session user follows that user's links as the
// kernel does, an absolute target included, and names the folder it opened
// by its real path.
func TestOpenDirFollowsALinkWithAnAbsoluteTargetForTheBridgesOwnUser(t *testing.T) {
	tree := newLinkTree(t)
	root := openRoot(t)

	dir, err := OpenDirNoUserLinks(root, filepath.Join(tree.link, "sub"), Owner{})
	require.NoError(t, err)
	defer dir.Close()
	require.Equal(t, filepath.Join(tree.real, "sub"), dir.Name())

	dir, missing, err := OpenNearestDirNoUserLinks(root, filepath.Join(tree.link, "sub", "new", "deeper"), Owner{})
	require.NoError(t, err)
	defer dir.Close()
	require.Equal(t, filepath.Join(tree.real, "sub"), dir.Name())
	require.Equal(t, []string{"new", "deeper"}, missing)
}

// ResolveFolders resolves the folders above the last name, the names that
// do not exist yet included, and leaves the last name alone even when it is
// a link; Resolve follows that one too.
func TestResolveFoldersLeavesTheLastNameAndResolveFollowsIt(t *testing.T) {
	tree := newLinkTree(t)
	alias := filepath.Join(tree.real, "sub", "alias")
	require.NoError(t, os.Symlink(tree.home, alias))

	for path, want := range map[string]string{
		filepath.Join(tree.link, "sub", "notes.txt"):         filepath.Join(tree.real, "sub", "notes.txt"),
		filepath.Join(tree.link, "sub", "new", "deeper.txt"): filepath.Join(tree.real, "sub", "new", "deeper.txt"),
		filepath.Join(tree.link, "sub", "alias"):             alias,
		tree.link:                                            tree.link,
		"/":                                                  "/",
	} {
		got, err := ResolveFolders(t.Context(), path, Owner{})
		require.NoError(t, err, path)
		require.Equal(t, want, got, path)
	}

	got, err := Resolve(t.Context(), filepath.Join(tree.link, "sub", "alias"), Owner{})
	require.NoError(t, err)
	require.Equal(t, tree.home, got)
	got, err = Resolve(t.Context(), tree.link, Owner{})
	require.NoError(t, err)
	require.Equal(t, tree.real, got)
}

// A chain of links resolves, relative and absolute targets mixed; a .. in a
// target goes up from where the link leads, as the kernel takes it; a loop
// ends in ELOOP and a link to nothing in ENOENT.
func TestResolveChainsDotDotLoopsAndDanglingLinks(t *testing.T) {
	tree := newLinkTree(t)
	chain := filepath.Join(tree.home, "chain")
	require.NoError(t, os.Symlink("data", chain)) // relative, to the absolute link
	up := filepath.Join(tree.home, "up")
	require.NoError(t, os.Symlink(filepath.Join(tree.real, "sub", ".."), up))

	got, err := ResolveFolders(t.Context(), filepath.Join(chain, "sub", "x"), Owner{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tree.real, "sub", "x"), got)
	got, err = ResolveFolders(t.Context(), filepath.Join(up, "sub", "x"), Owner{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tree.real, "sub", "x"), got)

	a, b := filepath.Join(tree.home, "a"), filepath.Join(tree.home, "b")
	require.NoError(t, os.Symlink(b, a))
	require.NoError(t, os.Symlink(a, b))
	_, err = ResolveFolders(t.Context(), filepath.Join(a, "x"), Owner{})
	require.ErrorIs(t, err, unix.ELOOP)
	_, err = Resolve(t.Context(), a, Owner{})
	require.ErrorIs(t, err, unix.ELOOP)

	gone := filepath.Join(tree.home, "gone")
	require.NoError(t, os.Symlink(filepath.Join(tree.base, "nowhere"), gone))
	_, err = Resolve(t.Context(), filepath.Join(gone, "x"), Owner{})
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// For a root bridge acting for a session user a link in a folder anyone but
// root could write is refused, never followed, with the error that names
// where it points. Without root every folder of the test is such a folder.
func TestResolveRefusesALinkTheSessionUserCouldPlant(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("as root the test's folders are root's, whose links are followed")
	}
	tree := newLinkTree(t)
	session := Owner{UID: uint32(os.Getuid()) + 1, GID: uint32(os.Getgid()) + 1, Set: true}
	refusal := tree.link + " is a link to " + tree.real + ". Open that folder to make this change"

	_, err := ResolveFolders(t.Context(), filepath.Join(tree.link, "sub", "notes.txt"), session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)
	require.ErrorContains(t, err, refusal)
	_, err = Resolve(t.Context(), tree.link, session)
	require.ErrorIs(t, err, bridgeipc.ErrForbidden)

	// The link itself, as the last name, is not followed, so nothing is refused.
	got, err := ResolveFolders(t.Context(), tree.link, session)
	require.NoError(t, err)
	require.Equal(t, tree.link, got)
}

// A link at the last name whose target holds .. is taken as the kernel takes
// it: home/L -> c/../x, where home/c -> ../other/sub, leads to other/x, not
// home/x, as it does when the link is a folder on the way.
func TestALinkTargetWithDotDotIsTakenAsTheKernelTakesIt(t *testing.T) {
	tree := newLinkTree(t)
	other := filepath.Join(tree.base, "other")
	require.NoError(t, os.MkdirAll(filepath.Join(other, "sub"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(other, "x"), 0o755))
	require.NoError(t, os.Symlink("../other/sub", filepath.Join(tree.home, "c")))
	link := filepath.Join(tree.home, "L")
	require.NoError(t, os.Symlink("c/../x", link))
	want := filepath.Join(other, "x")

	got, err := Resolve(t.Context(), link, Owner{})
	require.NoError(t, err)
	require.Equal(t, want, got)
	got, err = ResolveFolders(t.Context(), filepath.Join(link, "y"), Owner{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(want, "y"), got)
	kernel, err := os.Stat(link)
	require.NoError(t, err)
	ours, err := os.Stat(want)
	require.NoError(t, err)
	require.True(t, os.SameFile(kernel, ours), "the kernel reaches the same folder")
}

// One resolution follows at most 40 links in all, as one path lookup in the
// kernel does, however they are spread over the last name's chain and the
// folders on the way: home/L0 -> f/L1 -> f/L2 ... with f -> . costs two links
// a hop.
func TestOneResolutionFollowsFortyLinksInAll(t *testing.T) {
	tree := newLinkTree(t)
	end := filepath.Join(tree.home, "end")
	require.NoError(t, os.Mkdir(end, 0o755))
	require.NoError(t, os.Symlink(".", filepath.Join(tree.home, "f")))
	chain := func(prefix string, hops int) string {
		for k := range hops {
			require.NoError(t, os.Symlink("f/"+prefix+strconv.Itoa(k+1), filepath.Join(tree.home, prefix+strconv.Itoa(k))))
		}
		require.NoError(t, os.Symlink("f/end", filepath.Join(tree.home, prefix+strconv.Itoa(hops))))
		return filepath.Join(tree.home, prefix+"0")
	}

	got, err := Resolve(t.Context(), chain("a", 19), Owner{}) // 1 + 2*19 + 1 = 40 links
	require.NoError(t, err)
	require.Equal(t, end, got)
	_, err = Resolve(t.Context(), chain("b", 20), Owner{}) // 42 links
	require.ErrorIs(t, err, unix.ELOOP)
}

// One resolution opens a bounded number of names, as os.Root bounds its own,
// however long the targets of the links it follows: three links whose
// targets each walk 1600 names exceed it, two do not.
func TestOneResolutionOpensABoundedNumberOfNames(t *testing.T) {
	tree := newLinkTree(t)
	require.NoError(t, os.Mkdir(filepath.Join(tree.home, "d"), 0o755))
	require.NoError(t, os.Symlink(strings.Repeat("d/../", 800)+".", filepath.Join(tree.home, "A")))

	got, err := ResolveFolders(t.Context(), filepath.Join(tree.home, "A", "A", "x"), Owner{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tree.home, "x"), got)
	_, err = ResolveFolders(t.Context(), filepath.Join(tree.home, "A", "A", "A", "x"), Owner{})
	require.ErrorIs(t, err, unix.ENAMETOOLONG)
}

// A path whose last folders do not exist yet is walked once: the names that
// exist are opened one at a time, not again for each missing level, so a
// deep new path costs one walk.
func TestResolveFoldersWalksAPathOnce(t *testing.T) {
	tree := newLinkTree(t)
	missing := strings.Repeat("/new", 200)
	w := walk{ctx: t.Context()}
	dir, names, err := w.folders(tree.link+missing, true)
	require.NoError(t, err)
	defer dir.Close()
	require.Equal(t, tree.real, dir.Name())
	require.Len(t, names, 200)
	// The names to the link, the names of its target and the first missing
	// name, each opened once; a walk per missing level would open them 200
	// times over.
	count := func(path string) int { return len(strings.Split(strings.Trim(path, "/"), "/")) }
	require.Equal(t, count(tree.link)+count(tree.real)+1, w.steps)
}

// cancelAfter is a context whose Err reports it cancelled from the call after
// the first after calls on.
type cancelAfter struct {
	context.Context
	calls, after int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.after {
		return context.Canceled
	}
	return nil
}

// A resolution stops between two names once its ctx is done, as a listing
// whose request was cancelled stops resolving the links in it.
func TestAResolutionStopsWhenItsContextIsDone(t *testing.T) {
	tree := newLinkTree(t)
	require.NoError(t, os.Mkdir(filepath.Join(tree.home, "d"), 0o755))
	require.NoError(t, os.Symlink(strings.Repeat("d/../", 800)+".", filepath.Join(tree.home, "A")))
	ctx := &cancelAfter{Context: t.Context(), after: 100}

	_, err := ResolveFolders(ctx, filepath.Join(tree.home, "A", "x"), Owner{})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 101, ctx.calls, "it stopped at the first check that saw the cancel")
}

// A target that ends in a slash names a folder, as the kernel takes it: a
// link to file/ fails with ENOTDIR rather than leading to the file, and a
// link to folder/ leads to the folder.
func TestALinkTargetEndingInASlashMustBeAFolder(t *testing.T) {
	tree := newLinkTree(t)
	file := filepath.Join(tree.home, "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	toFile := filepath.Join(tree.home, "L")
	require.NoError(t, os.Symlink("file/", toFile))
	toFolder := filepath.Join(tree.home, "F")
	require.NoError(t, os.Symlink("data/sub/", toFolder))

	_, kernelErr := os.Stat(toFile)
	require.ErrorIs(t, kernelErr, unix.ENOTDIR, "the kernel's answer")
	_, err := Resolve(t.Context(), toFile, Owner{})
	require.ErrorIs(t, err, unix.ENOTDIR)
	got, err := Resolve(t.Context(), toFolder, Owner{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tree.real, "sub"), got)
}
