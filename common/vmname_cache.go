package common

import (
	"log"
	"sync"
	"time"
)

var (
	vmNameMap     = make(map[string]string)
	vmNameMapLock = sync.RWMutex{}
)

func LoadVmNameMapFromCloudstack() {
	db, err := OpenMoldDB()
	if err != nil {
		log.Printf("DB 연결 실패: %v", err)
		return
	}
	defer db.Close()

	rows, err := db.Query("SELECT instance_name, name FROM vm_instance WHERE removed IS NULL")
	if err != nil {
		log.Printf("DB 쿼리 실패: %v", err)
		return
	}
	defer rows.Close()

	temp := make(map[string]string)
	for rows.Next() {
		var instanceName, vmName string // instance_name, name
		if err := rows.Scan(&instanceName, &vmName); err == nil {
			temp[instanceName] = vmName
		}
	}

	vmNameMapLock.Lock()
	vmNameMap = temp
	vmNameMapLock.Unlock()

	정
}

func GetVmNameMap() map[string]string {
	vmNameMapLock.RLock()
	defer vmNameMapLock.RUnlock()

	// 복사본 반환
	copy := make(map[string]string)
	for k, v := range vmNameMap {
		copy[k] = v
	}
	return copy
}

func StartVmNameMapAutoRefresh(interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			LoadVmNameMapFromCloudstack()
		}
	}()
}
