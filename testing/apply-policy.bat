@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion
REM ============================================================
REM  写入 Chrome / Edge 的 PNA / LNA 放行策略（方案2b）
REM  把指定的页面源加入白名单，使其可访问本地 loopback。
REM  支持多个来源：用英文分号 ; 分隔。
REM  每个来源在策略子键下写为一个独立编号值（1/2/3...），
REM  这是 Chrome/Edge 列表型策略在注册表中的标准格式，
REM  与打包脚本 setup_inno/go.iss 的写入逻辑保持一致。
REM  需要"以管理员身份运行"。
REM ============================================================

REM ====== 在这里改成你现场真实的页面源（协议+IP+端口）======
REM   单个：  set "ALLOW_ORIGINS=http://192.168.1.26:8002"
REM   多个：  用英文分号分隔
REM   全部：  set "ALLOW_ORIGINS=*"
set "ALLOW_ORIGINS=*"
REM ==========================================================

REM --- 检测管理员权限 ---
net session >nul 2>&1
if %errorLevel% neq 0 (
  echo [错误] 请右键"以管理员身份运行"本脚本！
  pause
  exit /b 1
)

echo.
echo ============================================================
echo  即将为以下来源写入 PNA/LNA 放行策略：
echo      %ALLOW_ORIGINS%
echo ============================================================
echo.

REM --- 4 个策略子键（Chrome/Edge × 新旧 PNA/LNA）---
set "KEY1=HKLM\SOFTWARE\Policies\Google\Chrome\InsecurePrivateNetworkRequestsAllowedForUrls"
set "KEY2=HKLM\SOFTWARE\Policies\Google\Chrome\LocalNetworkAccessAllowedForUrls"
set "KEY3=HKLM\SOFTWARE\Policies\Microsoft\Edge\InsecurePrivateNetworkRequestsAllowedForUrls"
set "KEY4=HKLM\SOFTWARE\Policies\Microsoft\Edge\LocalNetworkAccessAllowedForUrls"

REM --- 先清空旧的策略子键，避免覆盖时残留过期来源 ---
for %%K in ("%KEY1%" "%KEY2%" "%KEY3%" "%KEY4%") do (
  reg delete "%%~K" /f >nul 2>&1
)

REM --- 逐个子键写入全部来源（按分号拆分，编号 1/2/3...）---
for %%K in ("%KEY1%" "%KEY2%" "%KEY3%" "%KEY4%") do (
  call :WriteOrigins "%%~K"
)

echo.
echo [完成] 策略已写入。
echo   重要：必须【完全退出并重启】浏览器（关闭所有窗口）策略才生效。
echo   验证：在地址栏打开 chrome://policy 或 edge://policy ，
echo         确认上述策略出现且 Status 为 OK，再回测试页重新检测。
echo.
pause
endlocal
exit /b 0

REM ============================================================
REM  子过程：把 ALLOW_ORIGINS 按分号拆分，逐条写入指定子键。
REM  %~1 = 目标注册表子键
REM ============================================================
:WriteOrigins
set "TARGET=%~1"
set "IDX=0"
set "REMAIN=%ALLOW_ORIGINS%"

:SplitLoop
REM 取出第一个分号前的片段到 ITEM，其余留在 REMAIN
for /f "tokens=1* delims=;" %%a in ("!REMAIN!") do (
  set "ITEM=%%a"
  set "REMAIN=%%b"
)

REM 去掉 ITEM 首尾空格
for /f "tokens=* delims= " %%x in ("!ITEM!") do set "ITEM=%%x"

if not "!ITEM!"=="" (
  set /a IDX+=1
  reg add "!TARGET!" /v "!IDX!" /t REG_SZ /d "!ITEM!" /f >nul
  if !errorLevel! equ 0 (
    echo   [OK] !TARGET! ^| !IDX! = !ITEM!
  ) else (
    echo   [失败] !TARGET! ^| !IDX! = !ITEM!
  )
)

if not "!REMAIN!"=="" goto :SplitLoop
goto :eof
