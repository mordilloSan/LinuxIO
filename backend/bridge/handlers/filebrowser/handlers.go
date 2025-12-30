package filebrowser

func FilebrowserHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		"resource_get":          resourceGet,
		"resource_stat":         resourceStat,
		"resource_delete":       resourceDelete,
		"resource_post":         resourcePost,
		"resource_patch":        resourcePatch,
		"dir_size":              dirSize,
		"subfolders":            subfolders,
		"search":                searchFiles,
		"indexer_status":        indexerStatus,
		"chmod":                 resourceChmod,
		"users_groups":          usersGroups,
		"file_update_from_temp": fileUpdateFromTemp, // Used by code editor PUT
	}
}
