# User Validation Audit for FileBrowser

## Current Session User Flow

1. **Session Extraction** (all handlers start here)
   ```go
   d, err := newRequestContext(c)  // Gets session.User from context
   if err != nil {
       return Unauthorized
   }
   ```
   - `d.user.Username` - Session user's name (string)
   - `d.user.UID` - Session user's UID (string)
   - `d.user.GID` - Session user's GID (string)

2. **User Context Passed to Services**
   - `metadataService.FileInfoFaster(utils.FileOptions{Username: d.user.Username, ...})`
   - `fileService.WriteFile(utils.FileOptions{Username: d.user.Username, ...})`
   - Other services receive FileOptions with Username

## Issues Found

### Issue 1: Username in FileOptions Not Validated
**Current**: Services receive `FileOptions.Username` but don't validate it against session user
**Risk**: If FileOptions.Username is manipulated before service call, services might act as wrong user

**Affected Services**:
- `metadata_service.go` - FileInfoFaster, GetDirInfo (only receive username, don't validate)
- `file_service.go` - All methods receive but don't validate
- `archive_service.go` - Indirectly via metadata service

### Issue 2: No UID/GID Validation in Services
**Current**: Services don't check if session UID/GID matches file ownership
**Risk**: Could potentially allow operations on files owned by other users

**Example**: File owned by UID 1000 could be modified by session user with UID 1001

### Issue 3: No Validation Before File Operations
**Current**: Handlers pass username to services, but services immediately use it for filesystem ops
**Risk**: No coherence check between session user and actual filesystem operations

## Recommended Fixes

### Fix 1: Add UserContext Struct to Pass Session Info
```go
type UserContext struct {
    Username string  // Session username (e.g., "miguelmariz")
    UID      string  // Session UID (e.g., "1000")
    GID      string  // Session GID (e.g., "1000")
}
```

### Fix 2: Add Validation to All Services
Each service should validate that the username matches session:

```go
// In metadata_service.go
func (s *MetadataService) FileInfoFaster(userCtx UserContext, opts utils.FileOptions) (*iteminfo.ExtendedFileInfo, error) {
    // VALIDATE: Ensure username coherence
    if userCtx.Username != opts.Path.Username {
        return nil, fmt.Errorf("user context mismatch: session user %q != requested user %q", userCtx.Username, opts.Path.Username)
    }

    // ... continue with operation
}
```

### Fix 3: Add UID/GID Validation for File Operations
When performing file operations, validate ownership:

```go
// Check if file is owned by current user before modifying
stat, err := os.Stat(path)
if err != nil {
    return err
}

// Extract file owner UID
fileStat := stat.Sys().(*syscall.Stat_t)
fileOwnerUID := strconv.FormatUint(uint64(fileStat.Uid), 10)

// Validate: Can only modify own files (unless privileged)
if fileOwnerUID != userCtx.UID && !userCtx.IsPrivileged {
    return fmt.Errorf("permission denied: file owned by UID %s, session user is %s", fileOwnerUID, userCtx.UID)
}
```

## Files Requiring Changes

### High Priority (Direct file operations)
- `services/file_service.go` - WriteFile, WriteDirectory, DeleteFiles
- `services/move_copy_service.go` - MoveResource, CopyResource
- `services/operations.go` - MoveFile, CopyFile

### Medium Priority (Metadata operations)
- `services/metadata_service.go` - FileInfoFaster, GetDirInfo
- `services/archive_service.go` - CreateZip, CreateTarGz

### Low Priority (Read-only operations)
- Currently reading metadata is less critical, but should still validate

## Implementation Strategy

1. **Create UserContext struct** in services package
2. **Add validation helper** to check user coherence
3. **Update all handler calls** to pass complete UserContext
4. **Update all service signatures** to require UserContext
5. **Add UID/GID validation** in file modification operations
6. **Add tests** to verify validation works

## Testing Requirements

Test cases needed:
- ✓ Session user can read own files
- ✓ Session user can modify own files
- ✓ Session user CANNOT modify other user's files
- ✓ Username mismatch is caught and rejected
- ✓ UID/GID mismatch is caught and rejected
- ✓ Operations on symlinks resolve ownership correctly

## Security Implications

Currently, there's a **potential privilege escalation** if:
1. A malicious actor could craft a request with manipulated FileOptions
2. Services don't validate the username matches session user
3. File operations proceed without checking actual file ownership

Adding proper UserContext validation **closes this attack surface**.
