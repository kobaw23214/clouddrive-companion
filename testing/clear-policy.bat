@echo off
chcp 65001 >nul
REM ============================================================
REM  清除上面写入的 PNA / LNA 放行策略
REM  用于"默认行为"测试（写注册表前的对照组）。
REM  需要"以管理员身份运行"。
REM ============================================================
setlocal

net session >nul 2>&1
if %errorLevel% neq 0 (
  echo [错误] 请右键"以管理员身份运行"本脚本！
  pause
  exit /b 1
)

echo 正在清除 Chrome / Edge 的 PNA/LNA 放行策略...

reg delete "HKLM\SOFTWARE\Policies\Google\Chrome\InsecurePrivateNetworkRequestsAllowedForUrls" /f >nul 2>&1
reg delete "HKLM\SOFTWARE\Policies\Google\Chrome\LocalNetworkAccessAllowedForUrls" /f >nul 2>&1
reg delete "HKLM\SOFTWARE\Policies\Microsoft\Edge\InsecurePrivateNetworkRequestsAllowedForUrls" /f >nul 2>&1
reg delete "HKLM\SOFTWARE\Policies\Microsoft\Edge\LocalNetworkAccessAllowedForUrls" /f >nul 2>&1

echo.
echo [完成] 策略已清除。同样需要【完全重启浏览器】后再测"默认行为"。
echo.
pause
endlocal
