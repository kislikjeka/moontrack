ALTER TABLE transactions ADD COLUMN wallet_id UUID REFERENCES wallets(id);
CREATE INDEX idx_transactions_wallet_id ON transactions(wallet_id);
