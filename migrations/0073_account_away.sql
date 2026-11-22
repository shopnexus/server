-- Until when the person is away (tạm nghỉ): buyers cannot offer, accept or
-- ask for COD on their listings before then. NULL when not away; a past
-- time reads as not away.
ALTER TABLE accounts ADD COLUMN away_until timestamptz;
