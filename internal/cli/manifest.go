package cli

// VersionInfo is the version of a published pack.
type VersionInfo struct {
	Version   int    `json:"version"`
	Changelog string `json:"changelog,omitempty"`
}

// ManifestFile is a single file carried by a skill manifest.
type ManifestFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// SkillManifest describes a published skill.
type SkillManifest struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Version     VersionInfo    `json:"version"`
	Source      string         `json:"source"`
	Files       []ManifestFile `json:"files,omitempty"`
	Content     string         `json:"content,omitempty"` // SKILL.md body
}

// ExpertMember is a member pack of an expert pack.
type ExpertMember struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ExpertPack is the coordination pack of an expert pack.
type ExpertPack struct {
	Members []ExpertMember `json:"members"`
}

// ExpertManifest describes a published expert pack.
type ExpertManifest struct {
	Slug     string      `json:"slug"`
	Name     string      `json:"name"`
	Version  VersionInfo `json:"version"`
	Source   string      `json:"source"`
	Manifest ExpertPack  `json:"manifest"`
}

// MCPInstallManifest describes an installable MCP profile.
type MCPInstallManifest struct {
	Profile string            `json:"profile"`
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Version VersionInfo       `json:"version"`
	Source  string            `json:"source"`
}
