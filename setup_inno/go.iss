; ------------------------------------------------------------------
; --- CloudDrive Companion Installer (Per-User / 非管理员版) ---
; ------------------------------------------------------------------
; 适用场景：页面已升级为 HTTPS（安全上下文），无需再写浏览器 PNA/LNA 策略。
;   - 安全上下文下，老版 PNA 靠服务端 Access-Control-Allow-Private-Network 头放行；
;   - 新版 LNA(Chrome 142+) 由用户点一次"允许本地网络访问"授权。
; 因此本版本回到 per-user(lowest) 安装：
;   - 默认装到 %LOCALAPPDATA%\Programs（用户可写）→ 客户端能正常读写 config/日志/db；
;   - 协议注册在 HKCU，无需管理员，无 hive 错位问题。
; ------------------------------------------------------------------

; --- 1. 基本应用信息 ---
#define MyAppName "CloudDrive Companion"
#define MyAppVersion "1.0"
#define MyAppPublisher "My Company, Inc."
#define MyAppURL "https://www.wy.com/"
#define MyAppExeName "clouddrive-companion.exe"

[Setup]
; AppId 是此应用的唯一ID。不要在其他安装程序中使用相同的值。
AppId={{7BCF00D2-8F8E-4058-AE8D-B62594225D79}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
; 默认目录由 [Code] 的 GetDefaultInstallDir 决定：优先第一个非系统盘的固定磁盘
; （如 D:\CloudDrive Companion），找不到再回落 {autopf}（%LOCALAPPDATA%\Programs）。
; 升级安装时 Inno 会自动沿用旧目录（UsePreviousAppDir），不受此默认值影响。
DefaultDirName={code:GetDefaultInstallDir}
UninstallDisplayIcon={app}\{#MyAppExeName}
; 仅允许在 x64 或 Arm 64位 Windows 上运行
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; 我们不使用文件关联
ChangesAssociations=no
; 隐藏"开始菜单文件夹"页面
DisableProgramGroupPage=yes
; 关键：非管理员安装（为当前用户安装）。
; 这样 {autopf} 解析为 %LOCALAPPDATA%\Programs（用户可写），
; 客户端把 config/日志/数据库写在自身目录时不会因权限失败。
PrivilegesRequired=lowest
OutputBaseFilename=CloudDriveCompanion-Setup-User
SolidCompression=yes
WizardStyle=modern
Compression=lzma2
; 添加版本信息
VersionInfoVersion={#MyAppVersion}
VersionInfoCompany={#MyAppPublisher}
VersionInfoDescription=CloudDrive Companion Installer (Per-User)
; 输出目录（相对于 SourceDir）
OutputDir=build\setup
; --- 关键：设置源文件的基准目录为项目根目录 ---
SourceDir=..

; 检测到已安装同 AppId 时重新运行向导；升级时通常不询问卸载。
CloseApplications=yes


[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "chinesesimp"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"

[Tasks]
; 允许用户选择是否创建桌面图标
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
; ------------------------------------------------------------------
; --- 主程序文件（路径相对于 SourceDir = 项目根目录）---
; ------------------------------------------------------------------
Source: "build\bin\clouddrive-companion.exe"; DestDir: "{app}"; DestName: "{#MyAppExeName}"; Flags: ignoreversion
Source: "build\bin\config.json"; DestDir: "{app}"; Flags: ignoreversion
; ------------------------------------------------------------------
; --- WebView2 Runtime 文件夹（安装时复制） ---
; ------------------------------------------------------------------
Source: "runtime\*"; DestDir: "{app}\runtime"; Flags: ignoreversion recursesubdirs createallsubdirs
; ------------------------------------------------------------------

[Registry]
; ------------------------------------------------------------------
; --- 注册 my-clouddrive:// URI 协议 (当前用户 / HKCU) ---
; ------------------------------------------------------------------
; 非管理员安装，协议写入 HKCU\Software\Classes，对当前用户生效。
; 安装者即使用者，浏览器以当前用户拉起客户端，路径与 hive 一致。
Root: hkcu; Subkey: "Software\Classes\my-clouddrive"; ValueType: string; ValueName: ""; ValueData: "URL:my-clouddrive Protocol"; Flags: uninsdeletekey
Root: hkcu; Subkey: "Software\Classes\my-clouddrive"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""; Flags: uninsdeletekey
Root: hkcu; Subkey: "Software\Classes\my-clouddrive\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#MyAppExeName}"" ""%1"""; Flags: uninsdeletekey
; ------------------------------------------------------------------
; 注意：本版本不写浏览器 PNA/LNA 策略（HKLM 需要管理员）。
;       页面已 HTTPS，安全上下文下靠服务端响应头 + LNA 用户授权即可放行。
; ------------------------------------------------------------------

[Icons]
; 开始菜单快捷方式
Name: "{autoprograms}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
; 桌面快捷方式 (如果用户在 [Tasks] 中选择了)
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
; 安装完成后不自动运行程序（已禁用）
; Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; 卸载时删除日志文件
Type: files; Name: "{app}\*.log"
; 卸载时删除 runtime 文件夹
Type: filesandordirs; Name: "{app}\runtime"

[Code]
var
  UninstallChoice: Integer;

// ------------------------------------------------------------------
// 默认安装目录：优先非系统盘
// ------------------------------------------------------------------
const
  DRIVE_FIXED = 3;  // GetDriveTypeW 返回值之一：固定磁盘（排除 U盘/光驱/网络盘）

function GetDriveType(lpRootPathName: String): UINT;
  external 'GetDriveTypeW@kernel32.dll stdcall';

// 找第一个"非系统盘的固定磁盘"，返回如 'D:'；没有则返回 ''
function FindDataDrive(): String;
var
  SysDrive, Letter: String;
  i: Integer;
begin
  Result := '';
  SysDrive := Uppercase(ExpandConstant('{sd}'));   // 系统盘，如 'C:'
  for i := Ord('C') to Ord('Z') do
  begin
    Letter := Chr(i) + ':';
    if Letter = SysDrive then
      Continue;
    if GetDriveType(Letter + '\') = DRIVE_FIXED then
    begin
      Result := Letter;
      Exit;
    end;
  end;
end;

// DefaultDirName 的脚本常量（Inno 在向导初始化时调用）
function GetDefaultInstallDir(Param: String): String;
var
  Drive: String;
begin
  Drive := FindDataDrive();
  if Drive <> '' then
    Result := Drive + '\{#MyAppName}'
  else
    Result := ExpandConstant('{autopf}\{#MyAppName}');
end;

// ------------------------------------------------------------------
// 目录选择页：装到系统盘时警告（说明后果，确认后仍放行）
// ------------------------------------------------------------------
function NextButtonClick(CurPageID: Integer): Boolean;
var
  Dir, SysDrive: String;
begin
  Result := True;
  if CurPageID = wpSelectDir then
  begin
    Dir := Trim(WizardForm.DirEdit.Text);
    SysDrive := Uppercase(ExpandConstant('{sd}'));
    // 只判断 "X:" 开头的本地路径；UNC 等其他形式不拦
    if (Length(Dir) >= 2) and (Dir[2] = ':') and
       (Uppercase(Copy(Dir, 1, 2)) = SysDrive) then
    begin
      if MsgBox(
        '您选择的安装位置在系统盘（' + SysDrive + '）。' + #13#10#13#10 +
        '客户端会把上传任务清单、断点续传记录和运行日志写在安装目录下。' + #13#10 +
        '装在系统盘（尤其是 Program Files 这类受保护目录）时，客户端以普通用户' + #13#10 +
        '身份运行可能没有写入权限，导致：' + #13#10 +
        '  • 上传到一半写不了任务文件，断点续传记录丢失' + #13#10 +
        '  • 日志写不进去，出问题无法排查' + #13#10#13#10 +
        '建议改装到其他磁盘（如 D:\）。' + #13#10#13#10 +
        '仍要继续安装到系统盘吗？',
        mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDNO then
        Result := False;
    end;
  end;
end;

// 关闭正在运行的程序（包括其 WebView2 子进程）
procedure CloseRunningApplication();
var
  ResultCode: Integer;
begin
  Log('正在检查并关闭已运行的 CloudDrive Companion 及其子进程...');
  Exec('taskkill', '/F /T /IM clouddrive-companion.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  if ResultCode = 0 then
    Log('已成功关闭正在运行的 CloudDrive Companion 及其子进程')
  else
    Log('没有发现正在运行的 CloudDrive Companion 或关闭失败 (这是正常的)');
  Sleep(800);
end;

// 检测是否已安装旧版本
function GetUninstallString(): String;
var
  sUnInstPath: String;
  sUnInstallString: String;
begin
  sUnInstPath := ExpandConstant('Software\Microsoft\Windows\CurrentVersion\Uninstall\{#emit SetupSetting("AppId")}_is1');
  sUnInstallString := '';
  if not RegQueryStringValue(HKLM, sUnInstPath, 'UninstallString', sUnInstallString) then
    RegQueryStringValue(HKCU, sUnInstPath, 'UninstallString', sUnInstallString);
  Result := sUnInstallString;
end;

function IsUpgrade(): Boolean;
begin
  Result := (GetUninstallString() <> '');
end;

function UnInstallOldVersion(): Integer;
var
  sUnInstallString: String;
  iResultCode: Integer;
begin
  Result := 0;
  sUnInstallString := GetUninstallString();
  if sUnInstallString <> '' then begin
    sUnInstallString := RemoveQuotes(sUnInstallString);
    if Exec(sUnInstallString, '/SILENT /NORESTART /SUPPRESSMSGBOXES','', SW_HIDE, ewWaitUntilTerminated, iResultCode) then
      Result := 3
    else
      Result := 2;
  end else
    Result := 1;
end;

// 安装前检查
function InitializeSetup(): Boolean;
var
  ButtonPressed: Integer;
begin
  Result := True;

  CloseRunningApplication();

  if IsUpgrade() then
  begin
    ButtonPressed := MsgBox(
      '检测到您已经安装了 ' + '{#MyAppName}' + '。' + #13#10#13#10 +
      '点击"是"将先卸载旧版本再安装新版本' + #13#10 +
      '点击"否"将直接覆盖安装（保留配置文件）' + #13#10 +
      '点击"取消"将退出安装程序' + #13#10#13#10 +
      '您要先卸载旧版本吗？',
      mbConfirmation,
      MB_YESNOCANCEL
    );

    case ButtonPressed of
      IDYES: begin
        UninstallChoice := UnInstallOldVersion();
        if UninstallChoice <> 3 then
        begin
          MsgBox('卸载旧版本失败，安装程序将退出。', mbError, MB_OK);
          Result := False;
        end;
      end;
      IDNO: begin
        Result := True;
      end;
      IDCANCEL: begin
        Result := False;
      end;
    end;
  end;
end;

// 安装进度回调
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssInstall then
  begin
    Log('开始安装文件...');
    Log('正在复制 WebView2 Runtime 到安装目录...');
  end;

  if CurStep = ssPostInstall then
  begin
    Log('安装完成！');
    if DirExists(ExpandConstant('{app}\runtime')) then
      Log('WebView2 Runtime 已成功安装到: ' + ExpandConstant('{app}\runtime'));
  end;
end;

// 卸载前确认
function InitializeUninstall(): Boolean;
begin
  if MsgBox('确定要卸载 CloudDrive Companion 吗?', mbConfirmation, MB_YESNO) = IDYES then
    Result := True
  else
    Result := False;
end;
