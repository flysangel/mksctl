# Windows 平台層的共用 helper。由 PSRunner 內嵌進每支配方腳本，不單獨執行。
# 前文（ErrorActionPreference、StrictMode、trap）已由 PSRunner 在檔頭寫好。

# Invoke-Native 執行原生命令，非零離開碼就 throw。
# powercfg、reg.exe 等的非零離開碼不會觸發 trap，所以一律經這裡。
# PS 5.1 在 ErrorActionPreference=Stop 下，會把原生命令寫到 stderr 的每一行升級成終止錯誤，
# 所以呼叫期間暫時改 Continue，成敗只看 $LASTEXITCODE；stderr 原樣轉發到 stderr。
function Invoke-Native {
    param(
        [Parameter(Mandatory = $true, Position = 0)][string]$Exe,
        [Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments
    )
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $Exe @Arguments 2>&1 | ForEach-Object {
            if ($_ -is [System.Management.Automation.ErrorRecord]) { [Console]::Error.WriteLine($_.ToString()) } else { $_ }
        }
        $code = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $prev
    }
    if ($code -ne 0) {
        throw "$Exe 離開碼 $code"
    }
}

# Get-Fetched 呼叫 mksctl fetch，回傳已驗證檔案的路徑（只收 stdout，stderr 轉發）。
function Get-Fetched {
    param([Parameter(Mandatory = $true)][string]$Id)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $lines = @()
    try {
        & $env:MKSCTL_BIN fetch $Id 2>&1 | ForEach-Object {
            if ($_ -is [System.Management.Automation.ErrorRecord]) { [Console]::Error.WriteLine($_.ToString()) } else { $lines += [string]$_ }
        }
        $code = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $prev
    }
    if ($code -ne 0) {
        throw "mksctl fetch $Id 離開碼 $code"
    }
    return ($lines | Select-Object -Last 1)
}

# Set-ServiceStartup 設定服務的啟動類型。
function Set-ServiceStartup {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][ValidateSet('Automatic', 'Manual', 'Disabled')][string]$Mode
    )
    Set-Service -Name $Name -StartupType $Mode
}

# Get-ServiceStartupRaw 直讀 HKLM\SYSTEM\CurrentControlSet\Services\<名稱>\Start。
# 回傳原始數值（2 自動、3 手動、4 停用）。不經 Get-Service，因為它對延遲啟動等狀態不夠精確。
function Get-ServiceStartupRaw {
    param([Parameter(Mandatory = $true)][string]$Name)
    $key = "HKLM:\SYSTEM\CurrentControlSet\Services\$Name"
    return [int](Get-ItemProperty -LiteralPath $key -Name Start).Start
}

# Set-FirewallRuleAny 建立（或更新）一條放行指定 TCP 埠的輸入規則，不限設定檔（Profile Any）。
function Set-FirewallRuleAny {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][int]$Port
    )
    if (Get-NetFirewallRule -Name $Name -ErrorAction SilentlyContinue) {
        Remove-NetFirewallRule -Name $Name
    }
    New-NetFirewallRule -Name $Name -DisplayName $Name -Direction Inbound -Action Allow `
        -Protocol TCP -LocalPort $Port -Profile Any | Out-Null
}
