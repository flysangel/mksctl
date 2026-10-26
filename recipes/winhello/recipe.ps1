# winhello 的 PowerShell 部分：寫、讀、刪 %ProgramData%\mksctl-winhello.txt。

function winhello_path {
    return (Join-Path $env:ProgramData 'mksctl-winhello.txt')
}

function winhello_install {
    Set-Content -LiteralPath (winhello_path) -Value 'winhello' -Encoding ASCII
    Write-Output 'Info: winhello 已寫入標記檔'
}

function winhello_verify {
    $p = winhello_path
    if (-not (Test-Path -LiteralPath $p)) {
        throw "winhello 標記檔不存在：$p"
    }
    $v = (Get-Content -LiteralPath $p -Raw).Trim()
    if ($v -ne 'winhello') {
        throw "winhello 標記檔內容不符：$v"
    }
}

function winhello_restore {
    Remove-Item -LiteralPath (winhello_path) -Force -ErrorAction SilentlyContinue
    Write-Output 'Info: winhello 標記檔已移除'
}
