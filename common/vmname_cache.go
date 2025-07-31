package common

import (
	"database/sql"
	"log"
	"sync"

	_ "github.com/go-sql-driver/mysql"
)

var (
	vmNameMap     = make(map[string]string)
	vmNameMapLock = sync.RWMutex{}
)

func LoadVmNameMapFromCloudstack() {
	db, err := sql.Open("mysql", "cloud:Ablecloud1!@tcp(localhost:3306)/cloud")
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

	log.Printf("Mold VM 이름 %d개 로딩됨", len(temp))
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
