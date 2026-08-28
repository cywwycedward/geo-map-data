[CmdletBinding()]
param(
    [string]$DuckDbPath = 'D:\duckdb_cli-windows-amd64\duckdb.exe',
    [string]$Release = '2026-07-22.0',
    [string]$ResultsDirectory = (Join-Path $PSScriptRoot 'results'),
    [string]$HttpProxy = $env:HTTPS_PROXY,
    [switch]$PlanOnly
)

$ErrorActionPreference = 'Stop'

function ConvertTo-SqlLiteral {
    param([string]$Value)

    "'$($Value.Replace("'", "''"))'"
}

function Get-StacJson {
    param([string]$Uri)

    for ($attempt = 1; $attempt -le 3; $attempt++) {
        try {
            return Invoke-RestMethod -Uri $Uri -TimeoutSec 30
        }
        catch {
            if ($attempt -eq 3) {
                throw
            }
            Start-Sleep -Seconds $attempt
        }
    }
}

function Test-BboxIntersects {
    param(
        [double[]]$Bbox,
        [double]$West,
        [double]$South,
        [double]$East,
        [double]$North
    )

    $Bbox[0] -lt $East -and $Bbox[2] -gt $West -and
        $Bbox[1] -lt $North -and $Bbox[3] -gt $South
}

function Get-StacAssetUrls {
    param(
        [string]$Release,
        [string]$Theme,
        [string]$Type,
        [double]$West,
        [double]$South,
        [double]$East,
        [double]$North
    )

    $collectionUrl = "https://stac.overturemaps.org/$Release/$Theme/$Type/collection.json"
    $collection = Get-StacJson $collectionUrl
    $assetUrls = foreach ($link in @($collection.links | Where-Object { $_.rel -eq 'item' })) {
        $item = Get-StacJson $link.href
        if (Test-BboxIntersects $item.bbox $West $South $East $North) {
            $item.assets.aws.href
        }
    }

    @($assetUrls | Sort-Object -Unique)
}

if (-not (Test-Path -LiteralPath $DuckDbPath)) {
    throw "DuckDB was not found at: $DuckDbPath"
}

$cases = @(
    [PSCustomObject]@{ Name = 'boston_places'; Theme = 'places'; Type = 'place'; West = -71.068; South = 42.353; East = -71.058; North = 42.363 },
    [PSCustomObject]@{ Name = 'paris_places'; Theme = 'places'; Type = 'place'; West = 2.294; South = 48.850; East = 2.304; North = 48.860 },
    [PSCustomObject]@{ Name = 'tokyo_places'; Theme = 'places'; Type = 'place'; West = 139.755; South = 35.675; East = 139.765; North = 35.685 }
)

if (-not $PlanOnly) {
    New-Item -ItemType Directory -Force $ResultsDirectory | Out-Null
    # Installation happens before timing. It is intentionally excluded from the data-acquisition metric.
    & $DuckDbPath -c 'INSTALL httpfs; INSTALL spatial;' | Out-Host
    if ($LASTEXITCODE -ne 0) {
        throw 'DuckDB extension installation failed.'
    }
}

$timestamp = Get-Date -Format 'yyyyMMddTHHmmss'
$results = @()
foreach ($case in $cases) {
    $assets = Get-StacAssetUrls $Release $case.Theme $case.Type $case.West $case.South $case.East $case.North
    if ($assets.Count -eq 0) {
        throw "STAC did not return an intersecting asset for $($case.Name)."
    }

    if ($PlanOnly) {
        [PSCustomObject]@{
            Name = $case.Name
            Bbox = "$($case.West),$($case.South),$($case.East),$($case.North)"
            AssetCount = $assets.Count
            Assets = $assets -join [Environment]::NewLine
        } | Format-List
        continue
    }

    $output = Join-Path $ResultsDirectory "$($case.Name)-$timestamp.parquet"
    $assetList = ($assets | ForEach-Object { ConvertTo-SqlLiteral $_ }) -join ', '
    $proxySql = if ([string]::IsNullOrWhiteSpace($HttpProxy)) { '' } else { "SET http_proxy = $(ConvertTo-SqlLiteral $HttpProxy);" }
    $sql = @"
LOAD httpfs;
$proxySql
LOAD spatial;
SET enable_external_file_cache = false;
SET enable_http_metadata_cache = false;
COPY (
  SELECT id, names.primary AS name, categories.primary AS category, bbox, geometry
  FROM read_parquet([$assetList])
  WHERE bbox.xmin BETWEEN $($case.West) AND $($case.East)
    AND bbox.ymin BETWEEN $($case.South) AND $($case.North)
) TO $(ConvertTo-SqlLiteral $output) (FORMAT PARQUET, COMPRESSION ZSTD);
"@

    $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    $commandOutput = & $DuckDbPath -c $sql 2>&1
    $exitCode = $LASTEXITCODE
    $stopwatch.Stop()
    if ($exitCode -ne 0) {
        throw "DuckDB failed for $($case.Name): $($commandOutput -join [Environment]::NewLine)"
    }

    $rowsCsv = & $DuckDbPath -csv -c "SELECT count(*) AS rows FROM read_parquet($(ConvertTo-SqlLiteral $output));"
    if ($LASTEXITCODE -ne 0) {
        throw "Could not count rows in $output"
    }
    $rows = ($rowsCsv | ConvertFrom-Csv).rows
    $fileBytes = (Get-Item -LiteralPath $output).Length
    $results += [PSCustomObject]@{
        Name = $case.Name
        Release = $Release
        Bbox = "$($case.West),$($case.South),$($case.East),$($case.North)"
        AssetCount = $assets.Count
        Assets = $assets
        Output = $output
        Rows = [int64]$rows
        OutputBytes = [int64]$fileBytes
        CopyWallClockSeconds = [Math]::Round($stopwatch.Elapsed.TotalSeconds, 3)
    }
}

if (-not $PlanOnly) {
    $reportPath = Join-Path $ResultsDirectory "benchmark-$timestamp.json"
    [PSCustomObject]@{
        Timestamp = (Get-Date).ToUniversalTime().ToString('o')
        DuckDbVersion = (& $DuckDbPath -csv -c 'SELECT version() AS version;' | ConvertFrom-Csv).version
        Release = $Release
        Measurement = 'New DuckDB process per case; wall clock includes CLI startup, LOAD, remote scan, filtering, compression, and local GeoParquet write. INSTALL and STAC discovery are excluded.'
        Results = $results
    } | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8 $reportPath

    $results | Format-Table Name, AssetCount, Rows, OutputBytes, CopyWallClockSeconds -AutoSize
    "Detailed JSON report: $reportPath"
}
