-- Task permission modes are now Orole's own vocabulary: manual | auto | yolo (plus the
-- daemon-internal read-only plan). Stored Claude Code values map to the nearest mode:
-- "" / default -> manual (ask before anything not known safe), acceptEdits -> auto.
-- Nothing existing becomes yolo, and plan is left as it is.
UPDATE agent_sessions
SET data = json_set(
      data,
      '$.permissionMode',
      CASE json_extract(data, '$.permissionMode') WHEN 'acceptEdits' THEN 'auto' ELSE 'manual' END)
WHERE json_extract(data, '$.permissionMode') IS NULL
   OR json_extract(data, '$.permissionMode') IN ('', 'default', 'acceptEdits');
