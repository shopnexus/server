-- GET /v1/reports lists a reporter's reports, newest first, paged by ID.
CREATE INDEX reports_reporter ON reports (reporter_id, id);
