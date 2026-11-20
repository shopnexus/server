-- Why a failed read failed, in a sentence for the seller: the model's
-- reason it saw no item, or the error asking it gave.
ALTER TABLE listing_reads ADD COLUMN failure_reason text
    CHECK (failure_reason IS NULL OR (status = 'failed' AND char_length(failure_reason) <= 300));
