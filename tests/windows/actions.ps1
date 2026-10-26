# actions.ps1 — Windows actions 場地的實測 harness
# 用法：tests\windows\actions.ps1 [-Recipe <名稱>]
# 對 family == windows 且 venue == actions 的每個配方（或指定的一個）跑四段流程：
#   1. machine 配方：install 兩次、restore 兩次
#   2. machine 配方以 SYSTEM 再跑一次 install（一次性排程），再 restore
#   3. user 配方：以臨時本機帳號 install 兩次、restore 兩次，並用 reg load 回讀宣告值
#   4. user 配方以 SYSTEM 跑 install：exit 3，輸出含 system-user-scope
# 每步印 [OK] 或 [FAIL]，最後印 [FAIL] <n>，n 不為 0 時 exit 1。
# 結束碼：0 成功、1 執行失敗、2 用法錯誤、3 安裝前檢查有差異。
# 驅動：.github/workflows/windows.yml
param(
    [string]$Recipe = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$work = Join-Path $env:TEMP 'mksctl-actions'
# 臨時帳號要讀得到執行檔與寫得到輸出檔，所以放在公開目錄
$pub = 'C:\Users\Public\mksctl-actions'
$script:failCount = 0

function Write-Step {
    param([bool]$Ok, [string]$Message)
    if ($Ok) {
        Write-Output "[OK] $Message"
    }
    else {
        Write-Output "[FAIL] $Message"
        $script:failCount++
    }
}

# Invoke-NativeQuiet 執行原生命令、丟掉輸出，回傳離開碼。
# PS 5.1 在 EAP=Stop 下會把合流的 stderr 每行升成終止錯誤，所以呼叫期間暫時改 Continue，成敗只看離開碼。
function Invoke-NativeQuiet {
    param([string]$Exe, [string[]]$NativeArgs)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $Exe @NativeArgs 2>&1 | Out-Null
        return $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $prev
    }
}

function Show-Log {
    param([string]$Path)
    if (Test-Path -LiteralPath $Path) {
        Get-Content -LiteralPath $Path | ForEach-Object { Write-Output "    | $_" }
    }
}

# Invoke-Mks 以目前身分執行 mksctl.exe，回傳 @{ Code; Out }。
function Invoke-Mks {
    param([string[]]$MksArgs)
    $out = Join-Path $work 'out.txt'
    $err = Join-Path $work 'err.txt'
    $p = Start-Process -FilePath $exe -ArgumentList $MksArgs -NoNewWindow -Wait -PassThru `
        -RedirectStandardOutput $out -RedirectStandardError $err
    $text = ((Get-Content -LiteralPath $out -Raw), (Get-Content -LiteralPath $err -Raw)) -join "`n"
    return @{ Code = $p.ExitCode; Out = $text }
}

function Test-Mks {
    param([string]$Label, [string[]]$MksArgs, [int]$Want = 0)
    $r = Invoke-Mks $MksArgs
    Write-Step ($r.Code -eq $Want) "$Label（exit $($r.Code)，預期 $Want）"
    if ($r.Code -ne $Want) { $r.Out -split "`n" | ForEach-Object { Write-Output "    | $_" } }
}

# Invoke-AsSystem 用一次性排程以 SYSTEM 執行 mksctl.exe，回傳 @{ Code; Out }。
function Invoke-AsSystem {
    param([string[]]$MksArgs)
    $out = Join-Path $pub 'system-out.txt'
    Remove-Item -LiteralPath $out -Force -ErrorAction SilentlyContinue
    $name = 'mksctl-actions-system'
    $cmd = '/c ""{0}" {1} > "{2}" 2>&1"' -f $pubExe, ($MksArgs -join ' '), $out
    $action = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument $cmd
    $principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
    Register-ScheduledTask -TaskName $name -Action $action -Principal $principal -Force | Out-Null
    try {
        Start-ScheduledTask -TaskName $name
        $deadline = (Get-Date).AddMinutes(5)
        do {
            Start-Sleep -Seconds 2
            $info = Get-ScheduledTaskInfo -TaskName $name
            $state = (Get-ScheduledTask -TaskName $name).State
            # 267011 = 尚未執行、267009 = 執行中；其餘是 cmd.exe 的離開碼
            $done = ($state -eq 'Ready') -and ($info.LastTaskResult -ne 267011) -and ($info.LastTaskResult -ne 267009)
        } until ($done -or (Get-Date) -gt $deadline)
        if (-not $done) { return @{ Code = -1; Out = '排程逾時' } }
        $text = ''
        if (Test-Path -LiteralPath $out) { $text = Get-Content -LiteralPath $out -Raw }
        return @{ Code = [int64]$info.LastTaskResult; Out = $text }
    }
    finally {
        Unregister-ScheduledTask -TaskName $name -Confirm:$false -ErrorAction SilentlyContinue
    }
}

# Get-DeclaredKeys 從 recipes/<名稱>/recipe.yaml 抓 registry 的 key 與 name（只認 key:／name: 兩行）。
function Get-DeclaredKeys {
    param([string]$Name)
    $result = @()
    $key = $null
    foreach ($line in Get-Content -LiteralPath (Join-Path $repo "recipes\$Name\recipe.yaml")) {
        if ($line -match '^\s*-?\s*key:\s*(\S+)\s*$') { $key = $Matches[1].Trim('"', "'") }
        elseif ($key -and $line -match '^\s*name:\s*(\S+)\s*$') {
            $result += , @($key, $Matches[1].Trim('"', "'"))
            $key = $null
        }
    }
    return $result
}

# 清單：mksctl list --all 的三欄 <名稱> <venue> <family>
$null = New-Item -ItemType Directory -Force -Path $work, $pub
$exe = Join-Path $repo 'dist\mksctl.exe'
if (-not (Test-Path -LiteralPath $exe)) {
    Write-Output "Error: 找不到 $exe，先建置：go build -o dist\mksctl.exe ./cmd/mksctl"
    exit 2
}
$pubExe = Join-Path $pub 'mksctl.exe'
Copy-Item -LiteralPath $exe -Destination $pubExe -Force
$null = Invoke-NativeQuiet 'icacls.exe' @($pub, '/grant', 'Everyone:(OI)(CI)M')

$list = Invoke-Mks @('list', '--all')
if ($list.Code -ne 0) {
    Write-Output "Error: mksctl list --all 失敗"
    Write-Output $list.Out
    exit 1
}
$recipes = @()
foreach ($line in ($list.Out -split "`r?`n")) {
    $f = $line.Trim() -split '\s+'
    if ($f.Count -ge 3 -and $f[1] -eq 'actions' -and $f[2] -eq 'windows') {
        if ($Recipe -eq '' -or $Recipe -eq $f[0]) { $recipes += $f[0] }
    }
}
if ($recipes.Count -eq 0) {
    Write-Output "Error: 沒有符合的 actions 配方（-Recipe '$Recipe'）"
    exit 2
}

# 預裝配方清單
$preRestore = @()
$preConf = Join-Path $PSScriptRoot 'pre-restore.conf'
if (Test-Path -LiteralPath $preConf) {
    foreach ($line in Get-Content -LiteralPath $preConf) {
        $t = $line.Trim()
        if ($t -ne '' -and -not $t.StartsWith('#')) { $preRestore += $t }
    }
}

# 配方的 scope：從 recipe.yaml 找 scope: user，沒有就是 machine
function Get-Scope {
    param([string]$Name)
    $y = Get-Content -LiteralPath (Join-Path $repo "recipes\$Name\recipe.yaml") -Raw
    if ($y -match '(?m)^scope:\s*user\s*$') { return 'user' }
    return 'machine'
}

function Test-UserRecipe {
    param([string]$Name)
    $user = 'mksctl-t8'
    $pw = ConvertTo-SecureString ('Aa1!' + [guid]::NewGuid().ToString('N')) -AsPlainText -Force
    $cred = New-Object System.Management.Automation.PSCredential($user, $pw)
    Start-Service seclogon
    Get-LocalUser -Name $user -ErrorAction SilentlyContinue | Remove-LocalUser
    New-LocalUser -Name $user -Password $pw | Out-Null
    $hive = 'HKU\mksctl-hive'
    try {
        Add-LocalGroupMember -SID 'S-1-5-32-545' -Member $user
        $uout = Join-Path $pub 'user-out.txt'
        $uerr = Join-Path $pub 'user-err.txt'
        foreach ($step in @('install', 'install', 'restore', 'restore')) {
            # install 之後、restore 之前要回讀，所以第一次 install 後與第一次 restore 前檢查
            $p = Start-Process -FilePath $pubExe -ArgumentList @($step, $Name) -Credential $cred `
                -LoadUserProfile -WorkingDirectory $pub -Wait -PassThru `
                -RedirectStandardOutput $uout -RedirectStandardError $uerr
            Write-Step ($p.ExitCode -eq 0) "$Name：user $step（exit $($p.ExitCode)，預期 0）"
            if ($p.ExitCode -ne 0) { Show-Log $uout; Show-Log $uerr }
            if ($step -eq 'install' -and $script:userReadback -ne $true) {
                $script:userReadback = $true
                $profile = (Get-CimInstance Win32_UserProfile | Where-Object { $_.LocalPath -like "*\$user" }).LocalPath
                $dat = Join-Path $profile 'NTUSER.DAT'
                # 行程結束後 hive 非同步卸載，剛結束就掛可能「檔案使用中」，重試最多 10 次
                $loaded = $false
                for ($i = 0; $i -lt 10 -and -not $loaded; $i++) {
                    $loaded = ((Invoke-NativeQuiet 'reg.exe' @('load', $hive, $dat)) -eq 0)
                    if (-not $loaded) { Start-Sleep -Seconds 1 }
                }
                Write-Step $loaded "$Name：reg load 掛上 $user 的 NTUSER.DAT"
                if ($loaded) {
                    try {
                        $declared = @(Get-DeclaredKeys $Name)
                        Write-Step ($declared.Count -gt 0) "$Name：recipe.yaml 讀得到宣告的登錄值（$($declared.Count) 筆）"
                        foreach ($kv in $declared) {
                            $k = $kv[0] -replace '^HKCU', $hive
                            $qc = Invoke-NativeQuiet 'reg.exe' @('query', $k, '/v', $kv[1])
                            Write-Step ($qc -eq 0) "$Name：使用者 hive 讀得到 $($kv[0])\$($kv[1])"
                        }
                    }
                    finally {
                        [gc]::Collect(); [gc]::WaitForPendingFinalizers()
                        $uc = Invoke-NativeQuiet 'reg.exe' @('unload', $hive)
                        Write-Step ($uc -eq 0) "$Name：reg unload $hive"
                    }
                }
            }
        }
    }
    finally {
        $script:userReadback = $false
        $prof = Get-CimInstance Win32_UserProfile | Where-Object { $_.LocalPath -like "*\$user" }
        Remove-LocalUser -Name $user -ErrorAction SilentlyContinue
        if ($prof) { $prof | Remove-CimInstance -ErrorAction SilentlyContinue }
    }
}

$script:userReadback = $false

foreach ($name in $recipes) {
    Write-Output "Info: 配方 $name"
    $scope = Get-Scope $name
    if ($scope -eq 'machine') {
        if ($preRestore -contains $name) {
            Test-Mks "$name：預裝，先 restore 一次" @('restore', $name)
        }
        Test-Mks "$name：install 第 1 次" @('install', $name)
        Test-Mks "$name：install 第 2 次" @('install', $name)
        Test-Mks "$name：restore 第 1 次" @('restore', $name)
        Test-Mks "$name：restore 第 2 次" @('restore', $name)
        # 第 2 段：SYSTEM
        $r = Invoke-AsSystem @('install', $name)
        Write-Step ($r.Code -eq 0) "$name：SYSTEM install（exit $($r.Code)，預期 0）"
        if ($r.Code -ne 0) { $r.Out -split "`n" | ForEach-Object { Write-Output "    | $_" } }
        Test-Mks "$name：SYSTEM install 之後 restore" @('restore', $name)
        Write-Output "[SKIP] $name：user 流程（machine 配方）"
    }
    else {
        Write-Output "[SKIP] $name：machine 流程（user 配方）"
        # 第 3 段
        Test-UserRecipe $name
        # 第 4 段
        $r = Invoke-AsSystem @('install', $name)
        $ok = ($r.Code -eq 3) -and ($r.Out -match 'system-user-scope')
        Write-Step $ok "$name：SYSTEM install 被擋（exit $($r.Code)，預期 3，輸出含 system-user-scope）"
        if (-not $ok) { $r.Out -split "`n" | ForEach-Object { Write-Output "    | $_" } }
    }
}

Write-Output "[FAIL] $script:failCount"
if ($script:failCount -ne 0) { exit 1 }
exit 0
