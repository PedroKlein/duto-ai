def fail($message): error($message);

def required($value; $message):
  if $value == null then
    fail($message)
  else
    $value
  end;

def repository_name:
  (env.GITHUB_REPOSITORY // "")
  | split("/")
  | if length == 2 and .[1] != "" then .[1] else fail("repository evidence contradicts trusted context") end;

def ensure_repository_matches:
  if ((required(.repository.id; "repository evidence contradicts trusted context") | tostring) != (env.GITHUB_REPOSITORY_ID // "")
      or required(.repository.owner.login; "repository evidence contradicts trusted context") != (env.GITHUB_REPOSITORY_OWNER // "")
      or required(.repository.name; "repository evidence contradicts trusted context") != repository_name) then
    fail("repository evidence contradicts trusted context")
  else
    .
  end;

def universal($revision):
  {
    "event-name": env.GITHUB_EVENT_NAME,
    "repository-owner": env.GITHUB_REPOSITORY_OWNER,
    "repository-name": repository_name,
    "repository-id": env.GITHUB_REPOSITORY_ID,
    "actor": env.GITHUB_ACTOR,
    "actor-id": env.GITHUB_ACTOR_ID,
    "revision": $revision,
    "ref": env.GITHUB_REF,
    "workflow-revision": env.GITHUB_WORKFLOW_SHA,
    "host-run-id": env.GITHUB_RUN_ID
  };

def event_fields($revision):
  if env.GITHUB_EVENT_NAME == "workflow_dispatch" then
    {}
  elif env.GITHUB_EVENT_NAME == "schedule" then
    {}
  elif env.GITHUB_EVENT_NAME == "push" then
    if (required(.after; "push event missing revision evidence") | tostring) != $revision then
      fail("stale revision evidence for push event")
    else
      {}
    end
  elif env.GITHUB_EVENT_NAME == "pull_request" then
    {
      "subject-kind": "pull_request",
      "subject-number": required(.pull_request.number; "pull_request event missing subject number"),
      "base-revision": required(.pull_request.base.sha; "pull_request event missing base revision"),
      "head-revision": required(.pull_request.head.sha; "pull_request event missing head revision"),
      "base-repository-id": (required(.pull_request.base.repo.id; "pull_request event missing base repository") | tostring),
      "head-repository-id": (required(.pull_request.head.repo.id; "pull_request event missing head repository") | tostring),
      "fork": (.pull_request.head.repo.fork // false)
    }
  elif env.GITHUB_EVENT_NAME == "issues" then
    {
      "subject-kind": "issue",
      "subject-number": required(.issue.number; "issues event missing subject number")
    }
  elif env.GITHUB_EVENT_NAME == "issue_comment" then
    {
      "subject-kind": (if .issue.pull_request? == null then "issue" else "pull_request" end),
      "subject-number": required(.issue.number; "issue_comment event missing subject number"),
      "comment-id": (required(.comment.id; "issue_comment event missing comment id") | tostring)
    }
  else
    fail("unsupported event: \(env.GITHUB_EVENT_NAME)")
  end;

if type != "object" then
  {}
else
  .
  | ensure_repository_matches
  | (env.DUTO_ACTION_CHECKOUT_HEAD // "") as $revision
  | if $revision == "" then
      fail("checkout head revision is missing")
    else
      universal($revision) + event_fields($revision)
    end
end
