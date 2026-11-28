-- Garage replaces RustFS as the object store (2026-11-28); the s3 dashboard
-- is Garage's web UI.
UPDATE console_dashboards SET display_name = 'Garage' WHERE app = 's3' AND display_name = 'RustFS';
