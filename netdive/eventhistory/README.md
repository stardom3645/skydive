# 최근 변경 이력

## 기존 코드 조사 및 연결 지점

| 역할 | 기존 경로 / 재사용 방식 |
| --- | --- |
| SQLite 연결 | `netdive/database/database.go`: Analyzer 소유의 단일 연결, 기존 WAL/busy timeout 유지 |
| Migration | `schema_migrations`, `netdive/database/migrations.go`: v4 추가. 기존 수동 매핑 데이터 보존 |
| Infrastructure 갱신 | netlink/libvirt → graph node 갱신 → `DefaultGraphListener` |
| Kubernetes 갱신 | `topology/probes/k8s` resource cache/informer → graph; Node `K8s.ConditionStates.Ready`, Pod `K8s.Extra.status.phase` |
| Cluster lifecycle | `api/server/mold_kubernetes_api.go`의 기존 Mold 목록 조회 결과. 별도 polling 없음 |
| 수집 상태 | `setKubernetesCollectionState`: 기존 최종 결과만 관측, CONNECTED/SYNCING은 중간 상태이므로 제외 |
| LLDP | `topology/probes/lldp/lldp.go`의 layer2 edge 생성/삭제 관측. switchport ↔ device만 대상 |
| Manual CRUD | `netdive/database/manual_port_mapping.go`: SQL 성공 뒤 공통 비동기 writer 사용. AUTO 승계는 수동 매핑 수정 기록 |
| 식별자 | Infrastructure는 기존 `topology/tid.go`의 TID, LLDP 포트는 기존 결정적 graph ID, Kubernetes UID, Mold UUID, 수동 매핑 DB ID |
| VM 식별자 | 이미 읽고 있는 libvirt XML의 UUID를 메타데이터에 전달. XML을 추가 조회하지 않음 |
| 상태 판정 | 기존 UI `StatusSummary.ts` / `KubernetesTopologyBadgeAggregation.ts`, 서버 Kubernetes classifier 유지. 원본 상태 변화 자체에 새로운 severity 판정을 하지 않음 |
| UI 이동 | `App.focusRecentViewedNode` / `statusSummaryTarget` 재사용. TID/UUID로 현재 노드를 다시 찾음 |
| 스케줄러 | 공통 DB 보존 scheduler가 없어 writer의 24시간 ticker만 추가. 수집 polling은 추가하지 않음 |

## 저장 및 실패 정책

- 처음 확인한 상태는 메모리 baseline이며 DB에 INSERT하지 않는다. 재시작은 새 baseline으로 시작한다. 서비스 중단 중 발생한 변화는 복원하지 않는다.
- 같은 값 반복, 이름만 변경, informer 재전달은 상태 이벤트가 아니다. 정상적인 상태 변화는 1건씩 저장한다.
- 첫 LLDP 포트 연결은 baseline이다. 이후 연결 변경/소실만 기록한다. 소실은 10초 유예 후 확정하며 그 안에 동일 TID 연결이 돌아오면 두 이벤트 모두 생략한다. 기록 시각은 최초 소실 관측 시각이다.
- 10초 이상 실제 관측되지 않는 연결은 소실로 남는다. 장시간 연결 중단과 물리적 링크 소실을 장애로 단정하지 않는다.
- `event_history`에는 상태값과 최소 ID/연결 메타정보만 기록한다. metric/snapshot 저장 없음.
- 1,024건 bounded queue, nonblocking enqueue, 단일 writer. DB 실패 및 큐 포화는 WARN 후 버린다. 수집 경로를 대기시키거나 실패시키지 않는다. 이력은 best-effort이며 DB 장애·과부하·비정상 종료 중의 기록까지 보장하는 감사 로그가 아니다.
- Manual CRUD는 기존 의미를 유지한다. 변경 없는 PUT/반복 DELETE는 이벤트를 추가하지 않는다. 저장소 내부 mutex가 동시에 실행되는 CRUD의 이전/이후 비교를 직렬화한다.
- 30일 기본 보존. 시작 직후 DELETE하지 않고 24시간마다 1,000건 단위 짧은 DELETE를 최대 2초 수행한다. 대량 적체는 다음 정리 주기에 이어 처리한다. VACUUM/checkpoint 강제 실행 없음.

```yaml
custom:
  database:
    driver: sqlite3
    path: /usr/share/ablestack/ablestack-netdive/netdive.db
    journalMode: WAL
    busyTimeout: 5000
    eventHistoryRetentionDays: 30
```

## 조회 API 및 UI

`GET /api/events` — 기존 인증 및 `topology/read` 권한. DB 미설정 시 503.

필터: `from`, `to` (Unix seconds), `resource_type`, `resource_id`, `event_type`, `source`, `search` (자원 이름 부분 일치), `page`, `pageSize`.
기본 최근 24시간, 20건; 최대 pageSize 100, 조회 기간 366일. 정렬은 `occurred_at DESC, id DESC`로 동일 초의 이벤트 순서도 안정적으로 유지한다.
응답은 `{events, total, page, pageSize}`. count와 rows는 같은 read transaction에서 조회한다.

UI: `요약 → 최근 변경 이력`. 기존 상태 요약 숨김은 유지한다. 최근 24시간/7일/30일, 유형·변경·이름 검색, 서버 페이지네이션을 제공한다. 새로고침 전까지 조회 종료 시각을 고정해 페이지가 밀리지 않도록 한다. API 요청은 필터 변경/닫기 시 취소하고 실패 상태를 빈 이력과 구분한다.

현재 없는 노드/삭제된 수동 포트는 이동 비활성화. 기본 성공 값으로의 복구는 success 표현, 다른 변화는 neutral 표현이며 NIC DOWN에 새로운 문제 판정을 하지 않는다. 원본 이벤트에 공통 classifier의 problem verdict가 전달되는 경우 red 표현을 지원한다.
`EventHistory`의 `resourceId` 및 공통 API를 상세 패널에서도 재사용할 수 있다.

## 1차 범위

Host/VM/NIC/Bond/Bridge의 수집된 상태, Kubernetes Node Ready/Pod phase, Mold Cluster lifecycle, 확정된 Kubernetes 수집 상태, LLDP 포트-NIC 관계, 수동 CRUD/AUTO 승계를 기록한다.
Host/Agent에 신뢰할 수 있는 상태값이 없으면 추측하지 않는다. workload replica 및 모든 노드 생성/삭제 이벤트는 이번 범위에 포함하지 않는다.

## 검증 및 적용

`go test -race ./netdive/database ./netdive/eventhistory` 및 API 이벤트 테스트. 기존 수동 매핑 migration/CRUD 회귀 테스트 포함.
SQLite 3.34.1 공식 소스를 static library로 빌드하고 `go test -tags libsqlite3 ./netdive/database`로 실제 런타임 버전까지 확인했다 (`NETDIVE_EXPECT_SQLITE_VERSION=3.34.1`). 최신 전용 RETURNING/JSON 함수 등을 사용하지 않는다.

Analyzer 저장소와 `skydive-ui` 양쪽 변경을 함께 빌드/적용해야 한다. 기존 CCVM DB를 삭제하거나 미리 재생성하지 않는다. Analyzer가 시작 시 migration을 적용한다.
