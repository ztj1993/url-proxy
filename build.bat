@echo off
setlocal

echo Building for Darwin...
set CGO_ENABLED=0
set GOOS=darwin
set GOARCH=amd64
go build -ldflags="-w -s" -o _/uproxy_darwin_amd64 .

set GOARCH=arm64
go build -ldflags="-w -s" -o _/uproxy_darwin_arm64 .

echo Building for FreeBSD...
set GOOS=freebsd
set GOARCH=386
go build -ldflags="-w -s" -o _/uproxy_freebsd_386 .

set GOARCH=amd64
go build -ldflags="-w -s" -o _/uproxy_freebsd_amd64 .

echo Building for Linux...
set GOOS=linux
set GOARCH=amd64
go build -ldflags="-w -s" -o _/uproxy_linux_amd64 .

set GOARCH=386
go build -ldflags="-w -s" -o _/uproxy_linux_386 .

set GOARCH=arm64
go build -ldflags="-w -s" -o _/uproxy_linux_arm64 .

set GOARCH=arm
set GOARM=7
go build -ldflags="-w -s" -o _/uproxy_linux_arm7 .

set GOARM=6
go build -ldflags="-w -s" -o _/uproxy_linux_arm6 .

set GOARM=5
go build -ldflags="-w -s" -o _/uproxy_linux_arm5 .

set GOARM=
set GOARCH=mips
go build -ldflags="-w -s" -o _/uproxy_linux_mips .

set GOARCH=mipsle
go build -ldflags="-w -s" -o _/uproxy_linux_mipsle .

set GOARCH=mips
set GOMIPS=softfloat
go build -ldflags="-w -s" -o _/uproxy_linux_mips_softfloat .

set GOARCH=mipsle
set GOMIPS=softfloat
go build -ldflags="-w -s" -o _/uproxy_linux_mipsle_softfloat .

set GOMIPS=
set GOARCH=mips64
go build -ldflags="-w -s" -o _/uproxy_linux_mips64 .

set GOARCH=mips64le
go build -ldflags="-w -s" -o _/uproxy_linux_mips64le .

set GOARCH=mips64
set GOMIPS=softfloat
go build -ldflags="-w -s" -o _/uproxy_linux_mips64_softfloat .

set GOARCH=mips64le
set GOMIPS=softfloat
go build -ldflags="-w -s" -o _/uproxy_linux_mips64le_softfloat .

set GOMIPS=
set GOARCH=ppc64
go build -ldflags="-w -s" -o _/uproxy_linux_ppc64 .

set GOARCH=ppc64le
go build -ldflags="-w -s" -o _/uproxy_linux_ppc64le .

echo Building for NetBSD...
set GOOS=netbsd
set GOARCH=386
go build -ldflags="-w -s" -o _/uproxy_netbsd_386 .

set GOARCH=amd64
go build -ldflags="-w -s" -o _/uproxy_netbsd_amd64 .

echo Building for OpenBSD...
set GOOS=openbsd
set GOARCH=386
go build -ldflags="-w -s" -o _/uproxy_openbsd_386 .

set GOARCH=amd64
go build -ldflags="-w -s" -o _/uproxy_openbsd_amd64 .

echo Building for Windows...
set GOOS=windows
set GOARCH=amd64
go build -ldflags="-w -s" -o _/uproxy_windows_amd64.exe .

set GOARCH=386
go build -ldflags="-w -s" -o _/uproxy_windows_386.exe .

set GOARCH=arm64
go build -ldflags="-w -s" -o _/uproxy_windows_arm64.exe .

echo Build complete!
endlocal
