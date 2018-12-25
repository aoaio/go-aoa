package metrics

type DiskStats struct {
	ReadCount  int64
	ReadBytes  int64
	WriteCount int64
	WriteBytes int64
}
