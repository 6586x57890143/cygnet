#requires -Version 7.0
# Disposable, unfunded stagenet fixtures on Docker Desktop for Windows.
# No ports are published and all wallet/client containers have network none.
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'This fixture harness expects Windows Docker Desktop bind-mount semantics.' }
Set-Location (Join-Path $PSScriptRoot '..')
& docker buildx build --platform linux/amd64 --target verified --load -f docker/monero/Dockerfile -t cygnet-xmr-verify:test .
if ($LASTEXITCODE -ne 0) { throw 'Cannot build the verified-release fixture tools.' }
& docker buildx build --platform linux/amd64 --target wallet-rpc --load -f docker/monero/Dockerfile -t cygnet-xmr-rpc:guard-test .
if ($LASTEXITCODE -ne 0) { throw 'Cannot build the guarded wallet image.' }
$scratch = Join-Path $env:TEMP ('cygnet-xmr-smoke-' + [guid]::NewGuid().ToString('N'))
$secrets = Join-Path $scratch 'secrets'
$viewDirectory = Join-Path $scratch 'view'
$fullDirectory = Join-Path $scratch 'full'
$sourceDirectory = Join-Path $scratch 'source'
New-Item -ItemType Directory -Path $secrets,$viewDirectory,$fullDirectory,$sourceDirectory | Out-Null
$fixtureLabel = & docker inspect --format '{{index .Config.Labels "codex.task"}}' cygnet-xmr-offline-smoke 2>$null
if ($LASTEXITCODE -eq 0) {
    if ($fixtureLabel -ne 'cygnet-xmr-runtime') { throw 'Existing fixture name belongs to another task.' }
    & docker rm -f cygnet-xmr-offline-smoke | Out-Null
}
& docker run -d --name cygnet-xmr-offline-smoke --label codex.task=cygnet-xmr-runtime --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 512m --cpus 1 --tmpfs /tmp:size=32m,mode=1777 --mount "type=bind,source=$sourceDirectory,target=/wallet" --env HOME=/tmp --entrypoint /usr/local/bin/monero-wallet-rpc cygnet-xmr-rpc:guard-test --wallet-dir /wallet --offline --stagenet --rpc-bind-ip 127.0.0.1 --rpc-bind-port 18088 --rpc-login fixture:disposable-test-password --log-file /tmp/wallet.log --max-log-file-size 1048576 --max-log-files 1 | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Cannot start offline setup fixture.' }
Start-Sleep -Seconds 2
function Invoke-FixtureRPC([string]$method, [hashtable]$parameters) {
    $payload = @{jsonrpc='2.0'; id=1; method=$method; params=$parameters} | ConvertTo-Json -Compress -Depth 5
    $response = & docker run --rm --network container:cygnet-xmr-offline-smoke --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 128m --cpus 1 --entrypoint curl cygnet-xmr-verify:test --silent --show-error --digest --user fixture:disposable-test-password --max-time 20 --header 'Content-Type: application/json' --data-raw $payload http://127.0.0.1:18088/json_rpc
    if ($LASTEXITCODE -ne 0) { throw "Fixture RPC transport failed for $method" }
    $reply = ($response -join "`n") | ConvertFrom-Json
    if ($reply.error) { throw "Fixture RPC failed for $method with code $($reply.error.code)" }
    return $reply.result
}
Invoke-FixtureRPC 'create_wallet' @{filename='disposable-full'; password='disposable-test-password'; language='English'} | Out-Null
$primary = (Invoke-FixtureRPC 'get_address' @{account_index=0}).address
if ($primary.Length -ne 95 -or -not $primary.StartsWith('5')) { throw 'Disposable stagenet wallet did not initialize.' }
$viewKey = (Invoke-FixtureRPC 'query_key' @{key_type='view_key'}).key
Invoke-FixtureRPC 'close_wallet' @{} | Out-Null
Invoke-FixtureRPC 'generate_from_keys' @{filename='disposable-view'; password='disposable-test-password'; address=$primary; viewkey=$viewKey; restore_height=0} | Out-Null
Remove-Variable viewKey
Invoke-FixtureRPC 'store' @{} | Out-Null
Invoke-FixtureRPC 'close_wallet' @{} | Out-Null
foreach ($kind in @('view','full')) {
    $destination = if ($kind -eq 'view') { $viewDirectory } else { $fullDirectory }
    Copy-Item -LiteralPath (Join-Path $sourceDirectory "disposable-$kind") -Destination (Join-Path $destination 'support-view')
    Copy-Item -LiteralPath (Join-Path $sourceDirectory "disposable-$kind.keys") -Destination (Join-Path $destination 'support-view.keys')
}
[IO.File]::WriteAllText((Join-Path $secrets 'xmr-wallet-password'), 'disposable-test-password')
$configuration = @'
wallet-file=/wallet/support-view
password-file=/run/secrets/xmr-wallet-password
rpc-bind-ip=0.0.0.0
rpc-bind-port=18088
confirm-external-bind=1
rpc-login=fixture:disposable-test-password-for-tls-authentication
rpc-ssl=enabled
rpc-ssl-private-key=/run/secrets/xmr-rpc-tls.key
rpc-ssl-certificate=/run/secrets/xmr-rpc-ca.crt
daemon-address=https://node.invalid:18081
daemon-ssl=enabled
daemon-ssl-ca-certificates=/etc/ssl/certs/ca-certificates.crt
daemon-ssl-allow-chained=1
untrusted-daemon=1
log-file=/tmp/wallet-rpc.log
log-level=0
max-log-file-size=1048576
max-log-files=1
stagenet=1
'@
[IO.File]::WriteAllText((Join-Path $secrets 'xmr-wallet.conf'), $configuration)
& docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 128m --cpus 1 --tmpfs /tmp:size=16m,mode=1777 --mount "type=bind,source=$secrets,target=/out" --entrypoint openssl cygnet-xmr-verify:test req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc -sha256 -days 1 -keyout /out/xmr-rpc-tls.key -out /out/xmr-rpc-ca.crt -subj /CN=xmr-wallet -addext 'subjectAltName=DNS:xmr-wallet,IP:127.0.0.1'
if ($LASTEXITCODE -ne 0) { throw 'Cannot generate disposable TLS fixture.' }
# This key belongs only to an unfunded, network-none test fixture. Make it
# readable across Docker Desktop's host-file UID translation; production keys
# remain 0400 and owned by the wallet service UID as documented.
& docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 128m --cpus 1 --mount "type=bind,source=$secrets,target=/out" --entrypoint chmod cygnet-xmr-verify:test 0444 /out/xmr-rpc-tls.key
if ($LASTEXITCODE -ne 0) { throw 'Cannot set disposable certificate fixture permissions.' }
foreach ($kind in @('view','full')) {
    $walletDirectory = if ($kind -eq 'view') { $viewDirectory } else { $fullDirectory }
    $name = "cygnet-xmr-guard-$kind-smoke"
    $fixtureLabel = & docker inspect --format '{{index .Config.Labels "codex.task"}}' $name 2>$null
    if ($LASTEXITCODE -eq 0) {
        if ($fixtureLabel -ne 'cygnet-xmr-runtime') { throw 'Existing guard name belongs to another task.' }
        & docker rm -f $name | Out-Null
    }
    & docker run -d --name $name --label codex.task=cygnet-xmr-runtime --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 512m --cpus 1 --tmpfs /tmp:size=16m,mode=1777 --mount "type=bind,source=$walletDirectory,target=/wallet" --mount "type=bind,source=$secrets,target=/run/secrets,readonly" --env "CYGNET_XMR_PRIMARY=$primary" --env CYGNET_XMR_NETWORK=Stagenet cygnet-xmr-rpc:guard-test | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot start disposable guarded fixture.' }
}
for ($attempt=0; $attempt -lt 30; $attempt++) {
    $fullState = (& docker inspect --format '{{json .State}}' cygnet-xmr-guard-full-smoke) | ConvertFrom-Json
    if (-not $fullState.Running) { break }
    Start-Sleep -Seconds 1
}
$viewState = (& docker inspect --format '{{json .State}}' cygnet-xmr-guard-view-smoke) | ConvertFrom-Json
if ($fullState.Running -or $fullState.ExitCode -ne 1) { throw 'Guard did not reject the full wallet.' }
if (-not $viewState.Running) { & docker logs cygnet-xmr-guard-view-smoke; throw 'Guard did not start the view-only wallet.' }
Write-Output 'Full wallet rejected; view-only service started.'
& docker run --rm --network container:cygnet-xmr-guard-view-smoke --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 128m --cpus 1 --mount "type=bind,source=$secrets,target=/fixture,readonly" --entrypoint curl cygnet-xmr-verify:test --silent --show-error --retry 20 --retry-connrefused --retry-delay 1 --retry-max-time 30 --digest --user fixture:disposable-test-password-for-tls-authentication --cacert /fixture/xmr-rpc-ca.crt --max-time 5 --header 'Content-Type: application/json' --data-raw '{"jsonrpc":"2.0","id":1,"method":"get_height"}' https://127.0.0.1:18088/json_rpc
if ($LASTEXITCODE -ne 0) { throw 'Real wallet TLS/Digest request failed.' }
$testBinary = Join-Path $scratch 'chain-smoke.test'
$previousBuildEnvironment = @{CGO_ENABLED=$env:CGO_ENABLED; GOOS=$env:GOOS; GOARCH=$env:GOARCH}
try {
    $env:CGO_ENABLED='0'
    $env:GOOS='linux'
    $env:GOARCH='amd64'
    & go test -c -o $testBinary ./internal/chain
    if ($LASTEXITCODE -ne 0) { throw 'Cannot compile the real-binary client regression test.' }
} finally {
    foreach ($variable in $previousBuildEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($variable, $previousBuildEnvironment[$variable], 'Process')
    }
}
& docker run --rm --network container:cygnet-xmr-guard-view-smoke --read-only --cap-drop ALL --security-opt no-new-privileges:true --pids-limit 64 --memory 512m --cpus 1 --tmpfs /tmp:size=16m,mode=1777 --mount "type=bind,source=$secrets,target=/fixture,readonly" --mount "type=bind,source=$testBinary,target=/chain-smoke.test,readonly" --env CYGNET_XMR_SMOKE=disposable --env "CYGNET_XMR_SMOKE_ADDRESS=$primary" --entrypoint /chain-smoke.test cygnet-xmr-rpc:guard-test '-test.run=^TestMoneroReleasedWalletRPC$' '-test.v'
if ($LASTEXITCODE -ne 0) { throw 'Real-binary Go client regression failed.' }
[pscustomobject]@{Scratch=$scratch; FullWalletRejected=$true; ViewWalletStarted=$true; RealClientPassed=$true} | ConvertTo-Json
foreach ($name in @('cygnet-xmr-offline-smoke','cygnet-xmr-guard-view-smoke','cygnet-xmr-guard-full-smoke')) {
    $fixtureLabel = & docker inspect --format '{{index .Config.Labels "codex.task"}}' $name 2>$null
    if ($LASTEXITCODE -eq 0 -and $fixtureLabel -eq 'cygnet-xmr-runtime') { & docker rm -f $name | Out-Null }
}
