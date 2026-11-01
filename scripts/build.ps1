<#
.SYNOPSIS
  构建 CloudDrive Companion，可在官方 Go 工具链与 Win7 兼容工具链之间切换。

.DESCRIPTION
  两个构建目标：

    modern (默认)  官方 Go 工具链，产物要求 Windows 10 / Server 2016+。
                   配套 runtime/ 里的最新 WebView2 固定版本。

    win7           Win7 补丁工具链（go-legacy-win7 / go-win7），产物可在
                   Windows 7 SP1 ~ Windows 11 上运行。
                   配套 runtime_win7/ 里的 WebView2 109.0.1518.x。

  为什么 win7 目标需要另一套工具链：
    官方 Go 1.21 起要求 Windows 10+，编译产物在 Win7 上启动即失败。
    补丁工具链 revert 掉了破坏 Win7 的若干上游提交（ProcessPrng、
    LoadLibraryA 兜底、sysSocket 兜底等），其余与官方一致。
    下载地址见 doc/windows7-support.md。

.PARAMETER Target
  modern | win7

.PARAMETER LegacyGoRoot
  Win7 补丁工具链的 GOROOT。未传时读环境变量 GO_LEGACY_WIN7_ROOT。

.PARAMETER Installer
  构建完成后调用 Inno Setup 生成安装包（需要 PATH 里有 ISCC.exe）。

.EXAMPLE
  .\scripts\build.ps1
  .\scripts\build.ps1 -Target win7 -LegacyGoRoot C:\go-legacy-win7
  .\scripts\build.ps1 -Target win7 -Installer
#>
[CmdletBinding()]
param(
    [ValidateSet('modern', 'win7')]
    [string]$Target = 'modern',

    [string]$LegacyGoRoot = $env:GO_LEGACY_WIN7_ROOT,

    [switch]$Installer,

    [switch]$NoSelfCheck,

    # 强制用 internal/config.DefaultConfig 重新生成 build\bin\config.json，
    # 覆盖开发机上那份可能指向本地后端的配置。
    [switch]$ResetConfig
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeDir = if ($Target -eq 'win7') { 'runtime_win7' } else { 'runtime' }

Write-Host "=== CloudDrive Companion 构建 ===" -ForegroundColor Cyan
Write-Host "项目根目录 : $repoRoot"
Write-Host "构建目标   : $Target"
Write-Host "运行时目录 : $runtimeDir"
Write-Host ""

# --- 1. 工具链准备 ---------------------------------------------------
# 只修改当前进程的环境变量，不污染系统设置。
if ($Target -eq 'win7') {
    # setx 写的变量只对之后新建的进程生效。若当前会话是 setx 之前开的，
    # 进程环境里读不到，这里回落去读注册表中的用户级/系统级变量，
    # 免得用户被迫重开终端。
    if (-not $LegacyGoRoot) {
        foreach ($scope in @('User', 'Machine')) {
            $fromRegistry = [Environment]::GetEnvironmentVariable('GO_LEGACY_WIN7_ROOT', $scope)
            if ($fromRegistry) {
                $LegacyGoRoot = $fromRegistry
                Write-Host "从 $scope 级环境变量读取工具链路径（当前会话未加载该变量）" -ForegroundColor DarkGray
                break
            }
        }
    }
    if (-not $LegacyGoRoot) {
        throw "win7 目标需要补丁工具链。请传 -LegacyGoRoot <路径>，或设置环境变量 GO_LEGACY_WIN7_ROOT。"
    }
    $legacyGoExe = Join-Path $LegacyGoRoot 'bin\go.exe'
    if (-not (Test-Path $legacyGoExe)) {
        throw "在 $LegacyGoRoot 下找不到 bin\go.exe，请检查 -LegacyGoRoot 是否指向工具链根目录。"
    }

    $env:GOROOT = (Resolve-Path $LegacyGoRoot).Path
    $env:PATH = (Join-Path $env:GOROOT 'bin') + ';' + $env:PATH
    # 禁止 go 命令按 go.mod 的 toolchain 指令自动下载官方工具链，
    # 否则补丁工具链会被官方版本顶替，产物又跑不了 Win7。
    $env:GOTOOLCHAIN = 'local'

    Write-Host "使用 Win7 补丁工具链: $env:GOROOT" -ForegroundColor Yellow
}

$goVersion = (& go version) -join ''
Write-Host "go version : $goVersion"

if ($Target -eq 'win7') {
    # 补丁工具链的 go version 输出与官方完全一致（如 "go1.25.12 windows/amd64"），
    # 无法据此区分。改为检查源码里的补丁特征：
    #   官方 Go 1.21+ 用 ProcessPrng（Win7 上不存在，这是启动失败的原因之一）
    #   补丁版 revert 回 RtlGenRandom
    $osWindows = Join-Path $env:GOROOT 'src\runtime\os_windows.go'
    if (-not (Test-Path $osWindows)) {
        Write-Warning "找不到 $osWindows，无法确认是否为补丁工具链。"
    } else {
        $src = Get-Content $osWindows -Raw
        $hasRtlGenRandom = $src -match 'RtlGenRandom'
        $hasProcessPrng = $src -match 'ProcessPrng'
        if ($hasRtlGenRandom -and -not $hasProcessPrng) {
            Write-Host "补丁校验   : 通过（runtime 使用 RtlGenRandom，非 ProcessPrng）" -ForegroundColor Green
        } else {
            Write-Warning "补丁校验失败：runtime 里 RtlGenRandom=$hasRtlGenRandom ProcessPrng=$hasProcessPrng。"
            Write-Warning "这看起来是官方 Go，不是 Win7 补丁工具链 —— 产物在 Win7 上会启动失败。"
        }
    }
}
Write-Host ""

# --- 2. 运行时目录检查 -----------------------------------------------
# 缺失不阻断构建（程序会回退到系统 WebView2），但必须让人看见。
$runtimePath = Join-Path $repoRoot $runtimeDir
if (Test-Path $runtimePath) {
    $manifest = Get-ChildItem -Path $runtimePath -Filter '*.manifest' -File -ErrorAction SilentlyContinue |
        Select-Object -First 1
    $versionText = if ($manifest) { [IO.Path]::GetFileNameWithoutExtension($manifest.Name) } else { '未知（无 .manifest 文件）' }
    Write-Host "WebView2 固定版本: $versionText  ($runtimeDir)"

    if ($Target -eq 'win7' -and $versionText -notlike '109.*') {
        Write-Warning "win7 目标要求 WebView2 109.0.1518.x，当前 $runtimeDir 里是 $versionText —— 在 Win7 上会启动失败。"
    }
    if ($Target -eq 'modern' -and $versionText -like '109.*') {
        Write-Warning "modern 目标却打包了 109 版本，Win10+ 用户会被降级到旧内核。"
    }
} else {
    Write-Warning "$runtimeDir/ 不存在。产物将依赖系统已安装的 WebView2。"
    if ($Target -eq 'win7') {
        Write-Warning "Win7 机器需要系统已装 109.0.1518.x，否则程序无法启动。"
    }
}
Write-Host ""

# --- 3. 构建 ---------------------------------------------------------
Push-Location $repoRoot
try {
    Write-Host "执行 wails build ..." -ForegroundColor Cyan
    & wails build -trimpath -platform windows/amd64
    if ($LASTEXITCODE -ne 0) { throw "wails build 失败，退出码 $LASTEXITCODE" }

    $exePath = Join-Path $repoRoot 'build\bin\clouddrive-companion.exe'
    if (-not (Test-Path $exePath)) { throw "构建结束但未找到产物: $exePath" }

    # 反查产物实际使用的 Go 版本，确认工具链切换生效
    $embedded = (& go version $exePath) -join ''
    Write-Host ""
    Write-Host "产物: $exePath" -ForegroundColor Green
    Write-Host "产物 Go 版本: $embedded"

    # --- 4. 环境自检工具 -------------------------------------------
    # 用同一套工具链编译，随主程序一起分发。
    # 它能否在目标机器上启动，本身就是对工具链的第一道验证。
    if (-not $NoSelfCheck) {
        Write-Host ""
        Write-Host "构建环境自检工具 ..." -ForegroundColor Cyan
        $checkExe = Join-Path $repoRoot 'build\bin\win7-selfcheck.exe'
        & go build -ldflags '-s -w' -trimpath -o $checkExe ./cmd/win7check
        if ($LASTEXITCODE -ne 0) { throw "自检工具构建失败，退出码 $LASTEXITCODE" }
        Write-Host "自检工具: $checkExe" -ForegroundColor Green
        Write-Host "在目标机器的安装目录下运行它即可完成环境检查。"
    }

    # --- 5. 安装包（可选）------------------------------------------
    if ($Installer) {
        $iscc = Get-Command 'ISCC.exe' -ErrorAction SilentlyContinue
        if (-not $iscc) {
            throw "找不到 ISCC.exe（Inno Setup 编译器），请将其加入 PATH 后重试。"
        }

        # go.iss 的 [Files] 段依赖 build\bin\config.json，但该目录被 .gitignore 忽略，
        # wails build 也不生成它 —— 干净环境下必须先补上，否则 ISCC 直接失败。
        # 同时把即将打包的关键字段打出来，避免把开发机指向本地后端的配置误发给客户。
        $configPath = Join-Path $repoRoot 'build\bin\config.json'
        if ($ResetConfig -or -not (Test-Path $configPath)) {
            if ($ResetConfig) {
                Write-Host ""
                Write-Host "按 -ResetConfig 重新生成默认配置 ..." -ForegroundColor Cyan
            } else {
                Write-Host ""
                Write-Warning "build\bin\config.json 不存在，将用 internal/config 的默认值生成。"
            }
            & go run ./cmd/genconfig -o $configPath
            if ($LASTEXITCODE -ne 0) { throw "生成 config.json 失败，退出码 $LASTEXITCODE" }
        }

        Write-Host ""
        Write-Host "即将打包的配置 ($configPath):" -ForegroundColor Cyan
        $cfg = Get-Content $configPath -Raw | ConvertFrom-Json
        foreach ($key in @('backendUploadUrl', 'backendCompleteUrl', 'backendCommandUrl', 'backendCheckUrl', 'localServerPort', 'uriScheme')) {
            Write-Host ("  {0,-20} {1}" -f $key, $cfg.$key)
        }
        Write-Host "  ↑ 请确认这些地址是要发给客户的值（用 -ResetConfig 可重置为默认值）" -ForegroundColor Yellow

        Write-Host ""
        Write-Host "执行 Inno Setup（RuntimeDir=$runtimeDir）..." -ForegroundColor Cyan
        & $iscc.Source "/DRuntimeDir=$runtimeDir" (Join-Path $repoRoot 'setup_inno\go.iss')
        if ($LASTEXITCODE -ne 0) { throw "ISCC 失败，退出码 $LASTEXITCODE" }
        Write-Host "安装包输出目录: $(Join-Path $repoRoot 'build\setup')" -ForegroundColor Green
    }
}
finally {
    Pop-Location
}

Write-Host ""
Write-Host "=== 构建完成 ===" -ForegroundColor Green
