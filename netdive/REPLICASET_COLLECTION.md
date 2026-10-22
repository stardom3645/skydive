# Kubernetes ReplicaSet 수집 수정 및 운영 적용 절차

확인일: 2026-10-21. 이 작업에서는 운영 설정·RBAC·서비스·클러스터 객체를 변경하거나 재시작하지 않았다.

## 1. 원인과 수정 범위

- `config/config.go`의 `analyzer.topology.k8s.probes` 기본값에 `replicaset`이 빠져 있었다.
- `topology/probes/k8s/replicaset.go`의 `apps/v1` LIST/WATCH 수집기와 Deployment→ReplicaSet / ReplicaSet→Pod 연결기는 이미 구현되어 있었다. 목록에 없는 수집기가 초기화되지 않아 해당 연결기도 생성되지 않았다.
- 기본 목록에 `replicaset`을 추가했다. 기존 항목은 유지했다.
- `enabledK8sSubprobes`는 파일·환경변수의 덮어쓰기 이후 실제 수집기 생성 직전에 의존성을 계산한다. 명시적 목록에 `deployment`가 있으면 누락된 `replicaset`만 추가한다. 이미 있으면 다시 추가하지 않는다. Deployment를 제외한 명시적 목록에는 새로 강제하지 않는다.
- 기존 스토리지 3종 자동 추가는 그대로 유지한다. 빈 목록의 “등록된 모든 Probe 활성화” 의미도 유지한다. 원본 설정 슬라이스를 수정하지 않는다.
- Secret/API Key·kubeconfig 저장, 수집 대상 선택, 수집 주기, API 계약, Infrastructure Probe, 프론트엔드, 장애 판정은 변경하지 않았다.
- ReplicaSet은 그래프 메타데이터와 실제 소유 관계에 사용한다. 기존 프론트엔드는 이를 관계 해석에 보존하되 카드로 표시하지 않고 **워크로드 컨트롤러 → Pod** 구조를 유지한다. 관계가 확인되지 않으면 기존 `관계 미확인` 처리가 계속 적용된다.

## 2. 설정 우선순위와 운영 유효값

`config.LoadConfig`는 `--conf`로 전달된 YAML 파일을 순서대로 병합한다. 명시적 목록은 기본 목록을 대체한다. `SKYDIVE_ANALYZER_TOPOLOGY_K8S_PROBES` 환경변수도 파일 값을 덮어쓸 수 있다. 따라서 기본값 추가만으로 모든 설치 환경을 수정했다고 볼 수 없다.

새 코드의 의존성 처리로 과거 목록도 동작하지만, 설정 파일 자체에도 `replicaset`을 추가해 운영 설정과 실행 의도를 일치시키는 것을 권장한다. **기존 목록을 초기화하지 말고 한 항목만 추가한다.** 예시는 확인 당시 목록에 해당 항목만 넣은 것이다.

```yaml
analyzer:
  topology:
    k8s:
      probes:
        - cluster
        - namespace
        - node
        - pod
        - service
        - deployment
        - replicaset
        - daemonset
        - statefulset
        - ingress
        - networkpolicy
```

다른 Probe가 이미 있으면 함께 유지한다. 환경변수로 목록을 지정한다면 해당 값에도 `replicaset`을 추가해야 한다. 기존 프로토콜·포트·배포 방식은 그대로 사용한다.

`GET /api/config/analyzer.topology.k8s.probes`는 **설정에 기록된 목록**이다. 수집기 생성 시 보완한 의존성을 역으로 설정에 저장하지 않으므로, 과거 명시적 목록을 그대로 두면 API에는 `replicaset`이 없어도 새 코드의 실행 목록에는 포함될 수 있다. 실제 실행 여부는 시작 로그의 `NewProbe ... subprobes [...]`와 그래프의 ReplicaSet 노드를 함께 확인한다.

## 3. 실제 환경 조회 결과와 한계

확인 대상 Analyzer: `http://10.10.1.97:8082`.

| 항목 | 적용 전 조회 결과 |
|---|---|
| `analyzer.topology.probes` | `["k8s"]` |
| `analyzer.topology.k8s.probes` | 위 목록에서 `replicaset`만 없음 |
| 기본 kubeconfig 경로 | `/usr/share/ablestack/ablestack-netdive/kubeconfigs/selected.kubeconfig` |
| 원본 그래프 ReplicaSet 수 | **0개** |
| 대상 Deployment | `rt1230-app/api`, UID `c20fc6cd-fa08-4380-8fb0-297d8619909d` |
| Pod가 참조하는 ReplicaSet | `api-76c485fff7`, UID `4e5a0f07-83b3-4f47-bbd9-0cd3d26ee0f0` |
| 대상 클러스터 | `k8s-lifecycle31-v1359-gfs2-r13`, Mold ID `8817a27f-72ba-4764-a7cf-5ae2d6272e67` |

원본 그래프에는 위 ReplicaSet UID를 소유자로 참조하는 Pod 관측 노드가 있었다. 그래프에 남은 Pod 노드 수를 현재 Kubernetes 활성 Pod 수와 동일하게 취급하지 않았다.

기존 노드 상세 API의 대상 노드 조회는 HTTP 200이었고, 워크로드 집계가 완료되어 `assignedWorkloads`에 위 `api` Deployment UID가 반환되었다. 해당 집계는 전체 네임스페이스 ReplicaSet LIST가 성공해야 진행되며, 실제 Pod→ReplicaSet→Deployment UID를 따라간다. 따라서 **해당 상세 API 자격증명에서 ReplicaSet LIST와 소유 관계 역추적이 동작한다는 간접 근거**는 확인했다.

다만 토폴로지 수집기는 저장된 kubeconfig를 사용한다. 상세 API의 LIST 성공이나 다른 리소스의 WATCH 성공만으로 **수집기 자격증명의 ReplicaSet WATCH 권한**을 확정하지 않았다. 직접 사용할 수 있는 해당 수집기 kubeconfig가 로컬에 없어 실제 클러스터 WATCH 요청과 권한 검사는 실행하지 못했다.

**운영 적용·재시작을 수행하지 않았으므로 실제 운영 그래프의 관계 복원은 아직 미검증이다.** 적용 전후 비교를 실제 복원 완료로 보고하지 않는다.

## 4. RBAC 확인

저장소의 다음 배포 예제는 이미 `cluster-admin` 역할을 참조하므로 ReplicaSet 조회·감시 권한을 포함한다. 기존 권한을 더 넓히는 변경은 하지 않았다.

- `contrib/kubernetes/skydive.yaml`
- `contrib/charts/skydive-analyzer/templates/clusterrolebinging.yaml`

실제 설치는 Mold가 제공한 kubeconfig를 사용할 수 있으므로 위 예제와 동일한 권한이라고 가정하면 안 된다. Analyzer에서 **해당 클러스터의 저장된 kubeconfig**를 사용해 아래 조회를 수행한다. 기본 `selected.kubeconfig`가 항상 대상 클러스터라는 보장은 없다. 다중 클러스터 파일 경로는 `kubeconfigs/<Mold-cluster-ID>.kubeconfig`이며 서비스 실행 사용자에게만 읽기 권한을 유지한다.

```sh
# 대상 클러스터 예시. kubeconfig 내용이나 토큰은 출력·공유하지 않는다.
KUBECONFIG_PATH=/usr/share/ablestack/ablestack-netdive/kubeconfigs/8817a27f-72ba-4764-a7cf-5ae2d6272e67.kubeconfig
kubectl --kubeconfig="$KUBECONFIG_PATH" auth can-i list replicasets.apps --all-namespaces
kubectl --kubeconfig="$KUBECONFIG_PATH" auth can-i watch replicasets.apps --all-namespaces
kubectl --kubeconfig="$KUBECONFIG_PATH" get replicasets.apps --all-namespaces
kubectl --kubeconfig="$KUBECONFIG_PATH" --request-timeout=10s get --raw '/apis/apps/v1/replicasets?watch=true&resourceVersion=0&timeoutSeconds=5'
```

마지막 WATCH는 서버 기준 최대 5초의 읽기 요청이며 객체를 변경하지 않는다. 이벤트가 없어 출력 없이 종료될 수 있다. 403은 권한 문제이며, 클라이언트 시간 초과나 통신 실패를 권한 허용의 근거로 보지 않는다.

수집기는 전체 네임스페이스 LIST/WATCH를 사용한다. 실제 `can-i` 또는 요청이 거부될 때만, 현재 수집 계정에 연결된 ClusterRole에 다음 규칙을 추가하는 방안을 검토한다. 이 규칙은 문서 예시이며 이번 작업에서 운영에 적용하지 않았다.

```yaml
rules:
  - apiGroups: ["apps"]
    resources: ["replicasets"]
    verbs: ["list", "watch"]
```

수집기 자체에 필요한 추가 권한은 위 두 읽기 동작이다. 다른 리소스·쓰기 동작·새로운 `cluster-admin` 부여는 필요하지 않다. 단일 네임스페이스 Role만으로는 현재 전체 네임스페이스 수집 방식의 권한을 충족하지 못한다. 권한 수정 시 기존 규칙과 바인딩은 보존한다.

## 5. 운영 적용 순서 — 아직 실행하지 않음

1. **실제 서비스 설정 위치 확인.** 저장소의 `netdive/netdive-analyzer.service`는 아래 경로를 `--conf`로 지정한다. 설치 환경의 unit/drop-in과 다를 수 있으므로 먼저 확인한다.

   ```sh
   systemctl show netdive-analyzer.service -p ExecStart -p EnvironmentFiles
   ```

   저장소 기준 파일: `/usr/share/ablestack/ablestack-netdive/netdive/netdive-analyzer.yml`.

   서비스의 `Environment`/`EnvironmentFile` 또는 배포 시스템에서 `SKYDIVE_ANALYZER_TOPOLOGY_K8S_PROBES`를 지정하는지도 확인한다. 비밀 환경변수 전체를 출력하거나 공유하지 않는다.

2. **기존 설정을 백업하고** 실제 YAML 또는 목록을 덮어쓰는 환경변수에 `replicaset` 한 항목을 추가한다. 다른 수집 목록·데이터베이스·인증·Secret·kubeconfig·포트는 유지한다. 저장소의 `netdive/netdive.yml`은 Agent용 예시이고 `config_netdive.py`는 Agent 배포·재시작을 수행하므로 이번 적용에 사용하지 않는다.

3. **새 소스를 사용하는 경우 기존 Linux 릴리스 빌드·교체 절차를 사용한다.** 기존 기능의 build tag와 네이티브 의존성을 유지하고 `k8s`를 포함한다. 패키지 빌드 검증 결과만으로 배포용 전체 바이너리가 생성되었다고 판단하지 않는다.

   참고: 기존 실행 파일에도 ReplicaSet 수집기 자체는 있으므로 명시적 설정에 `replicaset`을 추가하고 Analyzer를 재시작하는 것만으로 활성화할 수 있는 구조다. 이번 기본값·의존성 보완 코드까지 반영하려면 새 실행 파일이 필요하다.

4. **동일 수집 계정으로 4절 LIST/WATCH 권한을 확인**한다. 권한이 충분하면 RBAC 변경은 하지 않는다.

5. 운영자가 정한 적용 시간에 **Analyzer만 재시작**한다.

   ```sh
   sudo systemctl restart netdive-analyzer.service
   systemctl is-active netdive-analyzer.service
   journalctl -u netdive-analyzer.service --since '5 minutes ago' --no-pager
   ```

   이는 사용자 실행 절차이며 이번 작업에서 실행하지 않았다. Agent나 Kubernetes 워크로드를 재시작할 필요는 없다. Analyzer 재시작 동안 UI 연결·토폴로지 재동기화가 발생할 수 있다.

6. 아래 설정 조회에서 명시적 목록에 `replicaset`이 있는지 확인한다. API 인증을 사용하는 설치 환경에서는 기존 인증 방식을 그대로 사용한다.

   ```sh
   curl --fail --silent --show-error http://10.10.1.97:8082/api/config/analyzer.topology.k8s.probes
   ```

   시작 로그의 `NewProbe ... subprobes [...]`에도 `replicaset`이 있어야 한다. 다중 클러스터라면 각 실행 대상의 수집기와 권한을 확인한다. `forbidden`·인증 실패·반복 LIST/WATCH 오류가 없는지도 확인한다.

7. **그래프와 UI 관계 복원 확인.** 원본 그래프에서 아래 항목을 별도로 확인한다.

   - 해당 클러스터의 `Type=replicaset` 노드 수가 실제 Kubernetes LIST와 맞는가.
   - `api-76c485fff7`의 `K8s.UID`와 Kubernetes ReplicaSet UID가 일치하는가.
   - `K8s.OwnerReferences`의 Deployment UID가 `c20fc6cd-fa08-4380-8fb0-297d8619909d`인가.
   - `RelationType=deployment` 연결의 Parent가 Deployment UID, Child가 해당 ReplicaSet UID인가.
   - `RelationType=replicaset` 연결의 Parent가 해당 ReplicaSet UID, Child가 실제 소유 Pod UID인가.
   - 각 UID의 노드가 하나씩만 있고, 재수신으로 같은 관계의 연결선이 중복되지 않는가.
   - UI에 별도 ReplicaSet 카드가 생기지 않고 `api` 아래 실제 현재 Pod가 배치되는가. 원본 관계가 복원되기 전에는 `관계 미확인`이 유지되는 것이 맞다.

   ReplicaSet이 추가되어도 **Desired / Current / Ready / Available 판정 기준은 바뀌지 않는다.** Ready 0/2 자체를 정상화하는 수정이 아니므로 해당 워크로드가 실제로 복구되지 않았다면 기존 문제 판정은 유지된다.

적용 후 실패하면 먼저 설정·RBAC·LIST/WATCH 오류를 확인한다. 롤백이 필요하면 기존 배포 절차대로 실행 파일과 백업 설정을 복원하고 Analyzer를 재시작한다. Kubernetes 객체나 데이터베이스를 삭제해 해결하지 않는다.

## 6. 회귀 테스트와 수집 전후 비교

새 테스트 파일:

- `config/k8s_probes_test.go`: 기본 목록 보존, YAML 덮어쓰기, 환경변수 덮어쓰기.
- `topology/probes/k8s/replicaset_collection_test.go`: 과거 명시적 목록에 대한 실제 생성자 실행, LIST/WATCH, UID 그래프 연결, 중복 수신, 삭제, 잘못된 UID·네임스페이스·비컨트롤러 소유자 배제.

모의 API 테스트는 production `newSingleK8sProbe` → REST LIST/WATCH → ResourceCache → 메타데이터 매핑 → 실제 graph linker를 실행한다.

| 단계 | ReplicaSet 노드 | Deployment→ReplicaSet | ReplicaSet→Pod |
|---|---:|---:|---:|
| ReplicaSet이 아직 수신되지 않음 | 0 | 0 | 0 |
| 실제 UID 소유자 ReplicaSet LIST 또는 WATCH 수신 | 1 | 1 | 2 |
| 동일 UID Add 재수신·Modified·보존 그래프 재등록 | 1 | 1 | 2 |
| 해당 ReplicaSet 삭제 | 0 | 0 | 0 |

삭제 후 Deployment와 Pod 노드는 유지된다. 관계가 없는 이름 유사 객체를 연결하지 않는다. 이 표는 **로컬 모의 API 결과**이며 운영 적용 후 결과가 아니다.

검증 명령:

```sh
go test -race -count=1 ./config ./topology/probes/k8s
go test -tags k8s ./api/server ./analyzer ./common ./netdive/database ./netdive/eventhistory ./topology/probes/k8s github.com/skydive-project/skydive/graffiti/http
go build -tags k8s ./config ./topology/probes/k8s ./api/server ./analyzer
```

위 테스트·race 검사·관련 백엔드 패키지 빌드는 통과했다. 프론트엔드는 수정하지 않고 기존 관계·계층 회귀 테스트 19개도 통과해, ReplicaSet 메타데이터 보존/카드 숨김과 Deployment→Pod 배치를 확인했다.

**배포용 전체 실행 파일 빌드의 제한:** 현재 작업 환경은 Darwin/arm64 Go 1.23.2다. 전체 `go build -tags k8s -o /tmp/netdive-replicaset-analyzer .`는 기존 `gopsutil` Darwin 코드의 `undefined: KinfoProc`로 실패했다. CGO를 끈 Linux 교차 빌드 역시 기존 `rawsocket` 빌드 제약으로 불가능했다. 이 문제를 우회하기 위해 운영 코드·의존성·빌드 태그를 변경하지 않았다. 따라서 Linux 릴리스 환경에서 **전체 바이너리 빌드와 적용 후 실제 관계 복원** 검증이 남아 있다.
