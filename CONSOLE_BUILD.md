# NetDive 콘솔 기준 빌드

이 브랜치는 2026-05-28의 NetDive 콘솔 백엔드 기준 소스에 2026-05-26의 콘솔 UI 번들을 포함한다.

Linux x86_64 바이너리는 다음 명령으로 빌드한다.

```sh
docker build \
  --platform linux/amd64 \
  -f Dockerfile.console-build \
  --target artifact \
  --output type=local,dest=artifacts/linux-amd64 \
  .
```

결과 파일은 `artifacts/linux-amd64/ablestack-netdive`이다. ARM 기반 개발 장비에서는 amd64 에뮬레이션 때문에 빌드가 오래 걸리며 Docker에 최소 4GB 메모리를 권장한다.

빌드 중 `statics/ui_v2`를 `statics/bindata.go`에 다시 포함하므로, 콘솔 UI와 `/api/mold/vmconsole` 백엔드가 하나의 바이너리로 만들어진다.

실행 대상에는 다음 공유 라이브러리가 있어야 한다.

- `libpcap.so.0.8`
- `libvirt-lxc.so.0`
- `libvirt-qemu.so.0`
- `libvirt.so.0`
- `libxml2.so.2`
- `libc.so.6`
