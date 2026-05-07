ALTER TABLE subscription_tokens
    ADD COLUMN IF NOT EXISTS encrypted_token text;
