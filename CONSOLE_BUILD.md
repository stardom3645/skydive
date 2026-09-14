# NetDive 콘솔 기준 Linux 빌드

이 브랜치는 2026-05-28의 NetDive 콘솔 백엔드 기준 소스에 2026-05-26의 콘솔 UI 번들을 포함한다.

CCVM 배포용 바이너리는 Linux x86_64 빌드 환경에서 공식 릴리스와 동일한 정적 타깃으로 빌드한다. 일반 `make build` 또는 일반 `go build`를 사용하면 빌드 OS의 `libpcap.so` 등에 동적으로 연결되므로 사용하지 않는다.

```sh
make static \
  VERSION=0.27.0-console-20260528 \
  WITH_EBPF=false
```

결과 파일은 `$(go env GOPATH)/bin/skydive`이다. 배포 전에 다음 항목을 확인한다.

```sh
BIN="$(go env GOPATH)/bin/skydive"

file "$BIN"
ldd "$BIN" 2>&1 || true
readelf -d "$BIN" | grep NEEDED && echo "동적 링크 실패" || echo "정적 링크 확인"
"$BIN" version
strings "$BIN" | grep -E 'createConsoleEndpoint|/api/mold/vmconsole|mold.console.enabled'
```

`file`은 `statically linked`, `ldd`는 `not a dynamic executable`, `readelf` 검사 결과는 `정적 링크 확인`이어야 한다.

빌드 중 `statics/ui_v2`를 `statics/bindata.go`에 다시 포함하므로, 콘솔 UI와 `/api/mold/vmconsole` 백엔드가 하나의 바이너리로 만들어진다. 정적 타깃은 기존 공식 빌드처럼 CGO 기반 `libvirt` 태그를 제외한다.
