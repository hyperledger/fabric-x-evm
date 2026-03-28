-- blocks
CREATE TABLE
    IF NOT EXISTS blocks (
        block_number INTEGER PRIMARY KEY NOT NULL CHECK (block_number >= 0),
        block_hash BLOB NOT NULL UNIQUE CHECK (length (block_hash) = 32),
        parent_hash BLOB NOT NULL CHECK (length (parent_hash) = 32),
        state_root BLOB NOT NULL CHECK (length (state_root) = 32),
        timestamp BIGINT NOT NULL CHECK (timestamp >= 0),
        extra_data BLOB
    );

CREATE INDEX IF NOT EXISTS idx_blocks_hash ON blocks (block_hash);

-- transactions
CREATE TABLE
    IF NOT EXISTS transactions (
        tx_hash BLOB PRIMARY KEY CHECK (length (tx_hash) = 32),
        block_hash BLOB CHECK (length (block_hash) = 32),
        block_number BIGINT NOT NULL,
        tx_index INTEGER NOT NULL CHECK (tx_index >= 0),
        raw_tx BLOB NOT NULL,
        from_address BLOB NOT NULL CHECK (length (from_address) = 20),
        to_address BLOB CHECK (
            to_address IS NULL
            OR length (to_address) = 20
        ),
        contract_address BLOB CHECK (
            contract_address IS NULL
            OR length (contract_address) = 20
        ),
        status INTEGER NOT NULL CHECK (status IN (0, 1)), -- failed, success
        fabric_tx_id TEXT NOT NULL UNIQUE,
        fabric_tx_status INTEGER NOT NULL,
        FOREIGN KEY (block_number) REFERENCES blocks (block_number)
    );

CREATE UNIQUE INDEX IF NOT EXISTS idx_tx_block_pos ON transactions (block_number, tx_index);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tx_block_hpos ON transactions (block_hash, tx_index);

CREATE INDEX IF NOT EXISTS idx_txs_hash ON transactions (tx_hash);

-- Logs
-- denormalized for performance (relational queries are rare)
CREATE TABLE
    IF NOT EXISTS logs (
        block_number BIGINT NOT NULL,
        tx_hash BLOB NOT NULL CHECK (length (tx_hash) = 32),
        tx_index INTEGER NOT NULL CHECK (tx_index >= 0),
        log_index INTEGER NOT NULL CHECK (log_index >= 0),
        address BLOB NOT NULL CHECK (length (address) = 20),
        topic0 BLOB CHECK (
            topic0 IS NULL
            OR length (topic0) = 32
        ),
        topic1 BLOB CHECK (
            topic1 IS NULL
            OR length (topic1) = 32
        ),
        topic2 BLOB CHECK (
            topic2 IS NULL
            OR length (topic2) = 32
        ),
        topic3 BLOB CHECK (
            topic3 IS NULL
            OR length (topic3) = 32
        ),
        data BLOB NOT NULL,
        PRIMARY KEY (tx_hash, log_index),
        FOREIGN KEY (tx_hash) REFERENCES transactions (tx_hash),
        FOREIGN KEY (block_number) REFERENCES blocks (block_number)
    );

CREATE INDEX IF NOT EXISTS idx_logs_block ON logs (block_number);

CREATE INDEX IF NOT EXISTS idx_logs_address ON logs (address);

CREATE INDEX IF NOT EXISTS idx_logs_topic0 ON logs (topic0);