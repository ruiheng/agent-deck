//go:build windows

package sysinfo

// collectDisk is currently unavailable on native Windows in this code path.
func collectDisk() DiskStat {
	return DiskStat{}
}
