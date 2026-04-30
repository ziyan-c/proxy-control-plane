"""initial schema

Revision ID: 20260430_0001
Revises:
Create Date: 2026-04-30
"""

from collections.abc import Sequence

from alembic import op
import sqlalchemy as sa

revision: str = "20260430_0001"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "customers",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("email", sa.String(length=320), nullable=False),
        sa.Column("display_name", sa.String(length=160), nullable=True),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("email"),
    )
    op.create_index("ix_customers_status", "customers", ["status"])

    op.create_table(
        "proxy_nodes",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("name", sa.String(length=80), nullable=False),
        sa.Column("hostname", sa.String(length=255), nullable=False),
        sa.Column("public_host", sa.String(length=255), nullable=True),
        sa.Column("region", sa.String(length=80), nullable=True),
        sa.Column("protocol", sa.String(length=32), nullable=False),
        sa.Column("port", sa.Integer(), nullable=False),
        sa.Column("transport", sa.String(length=32), nullable=False),
        sa.Column("security", sa.String(length=32), nullable=False),
        sa.Column("enabled", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.UniqueConstraint("name"),
    )
    op.create_index("ix_proxy_nodes_enabled", "proxy_nodes", ["enabled"])

    op.create_table(
        "proxy_accounts",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("customer_id", sa.String(length=36), nullable=False),
        sa.Column("protocol", sa.String(length=32), nullable=False),
        sa.Column("uuid", sa.String(length=36), nullable=False),
        sa.Column("email_tag", sa.String(length=160), nullable=False),
        sa.Column("flow", sa.String(length=64), nullable=True),
        sa.Column("enabled", sa.Boolean(), nullable=False),
        sa.Column("expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("traffic_limit_bytes", sa.BigInteger(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["customer_id"], ["customers.id"], ondelete="CASCADE"),
        sa.UniqueConstraint("uuid"),
    )
    op.create_index("ix_proxy_accounts_customer_id", "proxy_accounts", ["customer_id"])
    op.create_index("ix_proxy_accounts_enabled", "proxy_accounts", ["enabled"])

    op.create_table(
        "proxy_account_nodes",
        sa.Column("proxy_account_id", sa.String(length=36), nullable=False),
        sa.Column("proxy_node_id", sa.String(length=36), nullable=False),
        sa.ForeignKeyConstraint(["proxy_account_id"], ["proxy_accounts.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["proxy_node_id"], ["proxy_nodes.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("proxy_account_id", "proxy_node_id"),
    )

    op.create_table(
        "subscription_tokens",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("customer_id", sa.String(length=36), nullable=False),
        sa.Column("name", sa.String(length=120), nullable=False),
        sa.Column("token_hash", sa.String(length=64), nullable=False),
        sa.Column("enabled", sa.Boolean(), nullable=False),
        sa.Column("expires_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("last_used_at", sa.DateTime(timezone=True), nullable=True),
        sa.ForeignKeyConstraint(["customer_id"], ["customers.id"], ondelete="CASCADE"),
        sa.UniqueConstraint("token_hash"),
    )
    op.create_index("ix_subscription_tokens_customer_id", "subscription_tokens", ["customer_id"])
    op.create_index("ix_subscription_tokens_enabled", "subscription_tokens", ["enabled"])

    op.create_table(
        "traffic_usage",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("proxy_account_id", sa.String(length=36), nullable=False),
        sa.Column("proxy_node_id", sa.String(length=36), nullable=False),
        sa.Column("upload_bytes", sa.BigInteger(), nullable=False),
        sa.Column("download_bytes", sa.BigInteger(), nullable=False),
        sa.Column("recorded_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["proxy_account_id"], ["proxy_accounts.id"], ondelete="CASCADE"),
        sa.ForeignKeyConstraint(["proxy_node_id"], ["proxy_nodes.id"], ondelete="CASCADE"),
    )
    op.create_index("ix_traffic_usage_account_recorded", "traffic_usage", ["proxy_account_id", "recorded_at"])

    op.create_table(
        "audit_logs",
        sa.Column("id", sa.String(length=36), primary_key=True),
        sa.Column("actor", sa.String(length=160), nullable=True),
        sa.Column("action", sa.String(length=160), nullable=False),
        sa.Column("metadata_json", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index("ix_audit_logs_created_at", "audit_logs", ["created_at"])


def downgrade() -> None:
    op.drop_index("ix_audit_logs_created_at", table_name="audit_logs")
    op.drop_table("audit_logs")
    op.drop_index("ix_traffic_usage_account_recorded", table_name="traffic_usage")
    op.drop_table("traffic_usage")
    op.drop_index("ix_subscription_tokens_enabled", table_name="subscription_tokens")
    op.drop_index("ix_subscription_tokens_customer_id", table_name="subscription_tokens")
    op.drop_table("subscription_tokens")
    op.drop_table("proxy_account_nodes")
    op.drop_index("ix_proxy_accounts_enabled", table_name="proxy_accounts")
    op.drop_index("ix_proxy_accounts_customer_id", table_name="proxy_accounts")
    op.drop_table("proxy_accounts")
    op.drop_index("ix_proxy_nodes_enabled", table_name="proxy_nodes")
    op.drop_table("proxy_nodes")
    op.drop_index("ix_customers_status", table_name="customers")
    op.drop_table("customers")

