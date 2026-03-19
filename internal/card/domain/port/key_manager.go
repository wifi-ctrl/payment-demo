package port

// KeyVersion DEK 版本元数据
type KeyVersion struct {
	Version int
	Status  string // "active" | "retiring" | "retired"
}

// KeyManager 密钥管理端口（被驱动端口）
type KeyManager interface {
	// CurrentDEK 获取当前活跃的 DEK 及其版本号
	CurrentDEK() (dek []byte, version int, err error)

	// DEKByVersion 按版本号获取 DEK（密钥轮换后解密旧数据用）
	DEKByVersion(version int) ([]byte, error)

	// HMACKey 获取 HMAC 密钥
	HMACKey() ([]byte, error)

	// RotateDEK 生成新 DEK 版本，旧 DEK 保留（状态→retiring）
	RotateDEK() (newVersion int, err error)

	// RetireDEK 将指定版本 DEK 标记为 retired
	RetireDEK(version int) error

	// ListVersions 列出所有 DEK 版本及状态
	ListVersions() ([]KeyVersion, error)
}
