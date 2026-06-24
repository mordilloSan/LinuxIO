package virt

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	libvirt "github.com/digitalocean/go-libvirt"
	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var (
	validCloudInitUsername = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	validCloudInitHostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	hostnameInvalidChars   = regexp.MustCompile(`[^a-z0-9-]+`)
)

type cloudInitUserData struct {
	Hostname      string               `yaml:"hostname"`
	ManageEtcHost bool                 `yaml:"manage_etc_hosts"`
	Users         []any                `yaml:"users"`
	SSHPWAuth     bool                 `yaml:"ssh_pwauth"`
	Chpasswd      *cloudInitChpassword `yaml:"chpasswd,omitempty"`
}

type cloudInitUser struct {
	Name              string   `yaml:"name"`
	Groups            []string `yaml:"groups,omitempty"`
	Shell             string   `yaml:"shell"`
	LockPasswd        bool     `yaml:"lock_passwd"`
	Sudo              string   `yaml:"sudo"`
	SSHAuthorizedKeys []string `yaml:"ssh_authorized_keys,omitempty"`
}

type cloudInitChpassword struct {
	Expire bool                      `yaml:"expire"`
	Users  []cloudInitChpasswordUser `yaml:"users"`
}

type cloudInitChpasswordUser struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password"`
	Type     string `yaml:"type"`
}

type seedImageFile struct {
	Name string
	Data []byte
}

func validateCloudInitRequest(req apischema.VMCreateRequest, preset vmImagePreset) error {
	username := strings.TrimSpace(req.CloudInitUsername)
	if username == "" {
		return badRequestf("%s requires cloudInitUsername", preset.Label)
	}
	if !validCloudInitUsername.MatchString(username) {
		return badRequestf("cloudInitUsername must start with a lowercase letter or underscore and contain only lowercase letters, numbers, dash, and underscore")
	}
	if strings.TrimSpace(req.CloudInitPassword) == "" && len(cloudInitSSHKeys(req.CloudInitSSHKey)) == 0 {
		return badRequestf("%s requires a cloud-init password or SSH key", preset.Label)
	}
	hostname := cloudInitHostname(req)
	if !validCloudInitHostname.MatchString(hostname) {
		return badRequestf("cloudInitHostname must be a valid single-label hostname")
	}
	return nil
}

func createManagedCloudInitSeed(ctx context.Context, conn libvirtConn, pool libvirt.StoragePool, seedName string, req apischema.VMCreateRequest, preset vmImagePreset) (createdVMVolume, error) {
	if err := ensureManagedVolumeAbsent(conn, pool, seedName); err != nil {
		return createdVMVolume{}, err
	}
	seedPath := filepath.Join(managedCloudPath, seedName)
	if _, statErr := os.Stat(seedPath); statErr == nil {
		return createdVMVolume{}, conflictf("managed volume path %q already exists", seedPath)
	}
	if seedErr := createCloudInitSeed(ctx, req, preset, seedPath); seedErr != nil {
		_ = removeFile(seedPath)
		return createdVMVolume{}, seedErr
	}
	if refreshErr := conn.StoragePoolRefresh(pool, 0); refreshErr != nil {
		_ = removeFile(seedPath)
		return createdVMVolume{}, fmt.Errorf("refresh default storage pool: %w", refreshErr)
	}
	if volume, lookupErr := conn.StorageVolLookupByPath(seedPath); lookupErr == nil {
		return createdVMVolume{Volume: volume, Name: seedName, Path: seedPath}, nil
	} else if !isStorageVolMissing(lookupErr) {
		return createdVMVolume{}, fmt.Errorf("look up cloud-init seed path: %w", lookupErr)
	}
	if volume, lookupErr := conn.StorageVolLookupByName(pool, seedName); lookupErr == nil {
		return createdVMVolume{Volume: volume, Name: seedName, Path: seedPath}, nil
	} else if !isStorageVolMissing(lookupErr) {
		return createdVMVolume{}, fmt.Errorf("look up cloud-init seed volume: %w", lookupErr)
	}
	return createdVMVolume{
		Volume: libvirt.StorageVol{Pool: defaultPoolName, Name: seedName, Key: seedPath},
		Name:   seedName,
		Path:   seedPath,
	}, nil
}

func createCloudInitSeedISO(_ context.Context, req apischema.VMCreateRequest, preset vmImagePreset, destination string) error {
	if err := validateCloudInitRequest(req, preset); err != nil {
		return err
	}
	userData, userDataErr := buildCloudInitUserData(req, preset)
	if userDataErr != nil {
		return userDataErr
	}
	metaData := []byte(fmt.Sprintf("instance-id: linuxio-%s\nlocal-hostname: %s\n", cloudInitHostname(req), cloudInitHostname(req)))
	image, imageErr := buildCloudInitSeedImage([]seedImageFile{
		{Name: "user-data", Data: userData},
		{Name: "meta-data", Data: metaData},
	})
	if imageErr != nil {
		return imageErr
	}

	tmpImage := destination + ".tmp"
	_ = removeFile(tmpImage)
	defer func() {
		_ = removeFile(tmpImage)
	}()
	if writeErr := os.WriteFile(tmpImage, image, 0o600); writeErr != nil {
		return writeErr
	}
	if renameErr := renameFile(tmpImage, destination); renameErr != nil {
		return fmt.Errorf("finalize cloud-init seed image: %w", renameErr)
	}
	if accessErr := makeManagedReadOnlyDiskAccessible(destination); accessErr != nil {
		return fmt.Errorf("prepare cloud-init seed image permissions: %w", accessErr)
	}
	return nil
}

func buildCloudInitUserData(req apischema.VMCreateRequest, preset vmImagePreset) ([]byte, error) {
	password := strings.TrimSpace(req.CloudInitPassword)
	keys := cloudInitSSHKeys(req.CloudInitSSHKey)
	user := cloudInitUser{
		Name:              strings.TrimSpace(req.CloudInitUsername),
		Groups:            preset.CloudInitGroups,
		Shell:             "/bin/bash",
		LockPasswd:        password == "",
		Sudo:              "ALL=(ALL) NOPASSWD:ALL",
		SSHAuthorizedKeys: keys,
	}
	data := cloudInitUserData{
		Hostname:      cloudInitHostname(req),
		ManageEtcHost: true,
		Users:         []any{"default", user},
		SSHPWAuth:     password != "",
	}
	if password != "" {
		data.Chpasswd = &cloudInitChpassword{
			Expire: false,
			Users: []cloudInitChpasswordUser{
				{Name: user.Name, Password: password, Type: "text"},
			},
		}
	}
	out, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}
	return append([]byte("#cloud-config\n"), out...), nil
}

func buildCloudInitSeedImage(files []seedImageFile) ([]byte, error) {
	const (
		bytesPerSector    = 512
		sectorsPerCluster = 4
		totalSectors      = 4096
		reservedSectors   = 1
		fatCopies         = 2
		fatSectors        = 12
		rootEntries       = 512
		mediaDescriptor   = 0xf8
	)
	rootDirSectors := (rootEntries * 32) / bytesPerSector
	rootDirSector := reservedSectors + fatCopies*fatSectors
	dataSector := rootDirSector + rootDirSectors
	clusterBytes := sectorsPerCluster * bytesPerSector
	dataOffset := dataSector * bytesPerSector
	cluster := uint16(2)

	image := make([]byte, totalSectors*bytesPerSector)
	writeFAT12BootSector(image, totalSectors, rootEntries, fatSectors, sectorsPerCluster, mediaDescriptor)
	if dataOffset >= len(image) {
		return nil, fmt.Errorf("cloud-init seed image layout has no data area")
	}
	maxDataBytes := len(image) - dataOffset

	fat := image[reservedSectors*bytesPerSector : (reservedSectors+fatSectors)*bytesPerSector]
	fat[0] = mediaDescriptor
	fat[1] = 0xff
	fat[2] = 0xff

	rootOffset := rootDirSector * bytesPerSector
	writeFATVolumeLabel(image[rootOffset:rootOffset+32], "CIDATA")
	rootEntryOffset := rootOffset + 32

	for idx, file := range files {
		if idx > 8 {
			return nil, badRequestf("too many cloud-init seed files")
		}
		clustersNeeded := (len(file.Data) + clusterBytes - 1) / clusterBytes
		if clustersNeeded == 0 {
			clustersNeeded = 1
		}
		usedDataBytes := int(cluster-2) * clusterBytes
		neededDataBytes := clustersNeeded * clusterBytes
		if usedDataBytes+neededDataBytes > maxDataBytes {
			return nil, badRequestf("cloud-init seed data exceeds %d bytes", maxDataBytes)
		}
		firstCluster := cluster
		for fileCluster := range clustersNeeded {
			currentCluster := cluster + uint16(fileCluster)
			if fileCluster == clustersNeeded-1 {
				setFAT12Entry(fat, currentCluster, 0x0fff)
			} else {
				setFAT12Entry(fat, currentCluster, currentCluster+1)
			}
		}
		for fileCluster := range clustersNeeded {
			currentCluster := firstCluster + uint16(fileCluster)
			clusterOffset := (dataSector + int(currentCluster-2)*sectorsPerCluster) * bytesPerSector
			start := fileCluster * clusterBytes
			end := min(start+clusterBytes, len(file.Data))
			copy(image[clusterOffset:clusterOffset+clusterBytes], file.Data[start:end])
		}

		shortName := cloudInitShortFATName(file.Name, idx)
		if err := writeFATLongNameEntry(image[rootEntryOffset:rootEntryOffset+32], file.Name, shortName); err != nil {
			return nil, err
		}
		rootEntryOffset += 32
		writeFATFileEntry(image[rootEntryOffset:rootEntryOffset+32], shortName, firstCluster, uint32(len(file.Data)))
		rootEntryOffset += 32
		cluster += uint16(clustersNeeded)
	}

	copy(image[(reservedSectors+fatSectors)*bytesPerSector:], fat)
	return image, nil
}

func writeFAT12BootSector(image []byte, totalSectors int, rootEntries int, fatSectors int, sectorsPerCluster int, mediaDescriptor byte) {
	copy(image[0:3], []byte{0xeb, 0x3c, 0x90})
	copy(image[3:11], "LINUXIO ")
	binary.LittleEndian.PutUint16(image[11:13], 512)
	image[13] = byte(sectorsPerCluster)
	binary.LittleEndian.PutUint16(image[14:16], 1)
	image[16] = 2
	binary.LittleEndian.PutUint16(image[17:19], uint16(rootEntries))
	binary.LittleEndian.PutUint16(image[19:21], uint16(totalSectors))
	image[21] = mediaDescriptor
	binary.LittleEndian.PutUint16(image[22:24], uint16(fatSectors))
	binary.LittleEndian.PutUint16(image[24:26], 63)
	binary.LittleEndian.PutUint16(image[26:28], 255)
	image[36] = 0x80
	image[38] = 0x29
	binary.LittleEndian.PutUint32(image[39:43], 0x4c494f01)
	copy(image[43:54], "CIDATA     ")
	copy(image[54:62], "FAT12   ")
	image[510] = 0x55
	image[511] = 0xaa
}

func writeFATVolumeLabel(entry []byte, label string) {
	copy(entry[0:11], fatPaddedName(label, 11))
	entry[11] = 0x08
}

func cloudInitShortFATName(name string, idx int) [11]byte {
	base := "SEED"
	if strings.HasPrefix(name, "user") {
		base = "USERDA"
	} else if strings.HasPrefix(name, "meta") {
		base = "METADA"
	}
	alias := fmt.Sprintf("%s~%d", base, idx+1)
	var out [11]byte
	copy(out[:], fatPaddedName(alias, 11))
	return out
}

func writeFATLongNameEntry(entry []byte, name string, shortName [11]byte) error {
	runes := utf16.Encode([]rune(name))
	if len(runes) > 13 {
		return badRequestf("cloud-init seed file name %q is too long", name)
	}
	entry[0] = 0x41
	entry[11] = 0x0f
	entry[13] = fatLongNameChecksum(shortName)
	writeFATLongNameRunes(entry, runes)
	return nil
}

func writeFATLongNameRunes(entry []byte, runes []uint16) {
	positions := []int{1, 3, 5, 7, 9, 14, 16, 18, 20, 22, 24, 28, 30}
	for idx, pos := range positions {
		value := uint16(0xffff)
		switch {
		case idx < len(runes):
			value = runes[idx]
		case idx == len(runes):
			value = 0
		}
		binary.LittleEndian.PutUint16(entry[pos:pos+2], value)
	}
}

func writeFATFileEntry(entry []byte, shortName [11]byte, firstCluster uint16, size uint32) {
	copy(entry[0:11], shortName[:])
	entry[11] = 0x20
	binary.LittleEndian.PutUint16(entry[26:28], firstCluster)
	binary.LittleEndian.PutUint32(entry[28:32], size)
}

func fatLongNameChecksum(shortName [11]byte) byte {
	var sum byte
	for _, value := range shortName {
		sum = ((sum & 1) << 7) + (sum >> 1) + value
	}
	return sum
}

func fatPaddedName(value string, length int) []byte {
	out := []byte(strings.ToUpper(value))
	if len(out) > length {
		out = out[:length]
	}
	for len(out) < length {
		out = append(out, ' ')
	}
	return out
}

func setFAT12Entry(fat []byte, cluster uint16, value uint16) {
	offset := int(cluster) * 3 / 2
	if cluster%2 == 0 {
		fat[offset] = byte(value)
		fat[offset+1] = (fat[offset+1] & 0xf0) | byte(value>>8&0x0f)
		return
	}
	fat[offset] = (fat[offset] & 0x0f) | byte(value<<4)
	fat[offset+1] = byte(value >> 4)
}

func cloudInitSSHKeys(raw string) []string {
	lines := strings.Split(raw, "\n")
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		key := strings.TrimSpace(line)
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func cloudInitHostname(req apischema.VMCreateRequest) string {
	hostname := strings.TrimSpace(req.CloudInitHostname)
	if hostname == "" {
		hostname = req.Name
	}
	hostname = strings.ToLower(hostname)
	hostname = hostnameInvalidChars.ReplaceAllString(hostname, "-")
	hostname = strings.Trim(hostname, "-")
	if len(hostname) > 63 {
		hostname = strings.Trim(hostname[:63], "-")
	}
	if hostname == "" {
		return "linuxio-vm"
	}
	return hostname
}
