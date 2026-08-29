CREATE TABLE invitations (
    id VARCHAR(36) PRIMARY KEY, tenant_id VARCHAR(36) NOT NULL, email VARCHAR(320) NOT NULL, token_hash VARCHAR(64) NOT NULL UNIQUE, status VARCHAR(32) NOT NULL DEFAULT 'pending', expires_at DATETIME(6) NOT NULL, accepted_by_user_id VARCHAR(36) NULL, version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
    INDEX idx_invitations_tenant_status (tenant_id, status, created_at DESC), CONSTRAINT fk_invitations_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE member_groups (
    id VARCHAR(36) PRIMARY KEY, tenant_id VARCHAR(36) NOT NULL, code VARCHAR(191) NOT NULL, name TEXT NOT NULL, status VARCHAR(32) NOT NULL DEFAULT 'active', version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
    UNIQUE KEY uq_member_groups_tenant_code (tenant_id, code), CONSTRAINT fk_member_groups_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE group_members (
    id VARCHAR(36) PRIMARY KEY, tenant_id VARCHAR(36) NOT NULL, group_id VARCHAR(36) NOT NULL, membership_id VARCHAR(36) NOT NULL, status VARCHAR(32) NOT NULL DEFAULT 'active', version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
    UNIQUE KEY uq_group_members_group_membership (group_id, membership_id), INDEX idx_group_members_membership (tenant_id, membership_id, status), CONSTRAINT fk_group_members_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE, CONSTRAINT fk_group_members_group FOREIGN KEY (group_id) REFERENCES member_groups(id) ON DELETE CASCADE, CONSTRAINT fk_group_members_membership FOREIGN KEY (membership_id) REFERENCES memberships(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE tenant_quotas (
    tenant_id VARCHAR(36) NOT NULL, quota_key VARCHAR(191) NOT NULL, limit_value BIGINT NOT NULL, used_value BIGINT NOT NULL DEFAULT 0, version BIGINT NOT NULL DEFAULT 1, created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL, created_by VARCHAR(255) NOT NULL, updated_by VARCHAR(255) NOT NULL,
    PRIMARY KEY (tenant_id, quota_key), CONSTRAINT fk_tenant_quotas_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE, CHECK (limit_value >= 0), CHECK (used_value >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
