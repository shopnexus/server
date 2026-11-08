package service

// IssueSession lets tests start sessions without the SignIn service.
var IssueSession = (*Account).issueSession
