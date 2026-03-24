-- name: BlockNumber :one
SELECT
    block_number
FROM
    blocks
ORDER BY
    block_number DESC
LIMIT
    1;

-- name: LatestBlock :one
SELECT
    *
FROM
    blocks
ORDER BY
    block_number DESC
LIMIT
    1;

-- name: BlockNumberByHash :one
SELECT
    block_number
FROM
    blocks
WHERE
    block_hash = ?
LIMIT
    1;

-- name: InsertBlock :exec
INSERT INTO
    blocks (
        block_number,
        block_hash,
        parent_hash,
        timestamp,
        extra_data
    )
VALUES
    (?, ?, ?, ?, ?) ON CONFLICT (block_number) DO NOTHING;

-- name: InsertTransaction :exec
INSERT INTO
    transactions (
        tx_hash,
        block_hash,
        block_number,
        tx_index,
        raw_tx,
        from_address,
        to_address,
        contract_address,
        status,
        fabric_tx_id,
        fabric_tx_status
    )
VALUES
    (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tx_hash) DO NOTHING;

-- name: GetTransactionByBlockNumberAndIndex :one
SELECT
    *
FROM
    transactions
WHERE
    block_number = ?
    AND tx_index = ?
LIMIT
    1;

-- name: GetTransactionByBlockHashAndIndex :one
SELECT
    *
FROM
    transactions
WHERE
    block_hash = ?
    AND tx_index = ?
LIMIT
    1;

-- name: GetFullTransactionByHash :one
SELECT
    *
FROM
    transactions
WHERE
    tx_hash = ?
LIMIT
    1;

-- name: GetTransactionByHash :one
SELECT
    *
FROM
    transactions
WHERE
    tx_hash = ?
LIMIT
    1;

-- name: GetBlockByNumber :one
SELECT
    *
FROM
    blocks
WHERE
    block_number = ?;

-- name: GetBlockByHash :one
SELECT
    *
FROM
    blocks
WHERE
    block_hash = ?;

-- name: GetBlockTxCountByNumber :one
SELECT
    COUNT(*) AS tx_count
FROM
    transactions
WHERE
    block_number = ?;

-- name: GetBlockTxCountByHash :one
SELECT
    COUNT(*) AS tx_count
FROM
    transactions t
    JOIN blocks b ON t.block_number = b.block_number
WHERE
    b.block_hash = ?;

-- name: GetTransactionsByBlockNumber :many
SELECT
    *
FROM
    transactions
WHERE
    block_number = ?
ORDER BY
    tx_index ASC;

-- name: GetTransactionsByBlockHash :many
SELECT
    t.*
FROM
    transactions t
    JOIN blocks b ON t.block_number = b.block_number
WHERE
    b.block_hash = ?
ORDER BY
    t.tx_index ASC;

-- name: InsertLog :exec
INSERT INTO
    logs (
        block_number,
        tx_hash,
        tx_index,
        log_index,
        address,
        topic0,
        topic1,
        topic2,
        topic3,
        data
    )
VALUES
    (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tx_hash, log_index) DO NOTHING;

-- name: GetLogsByTxHash :many
SELECT
    *
FROM
    logs
WHERE
    tx_hash = ?
ORDER BY
    log_index;