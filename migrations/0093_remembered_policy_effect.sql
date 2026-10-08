-- Older remembered allows did not retain their approved effect. Its original
-- strength cannot be reconstructed, so require consent again. Explicit rules
-- and remembered denials remain unchanged.
DELETE FROM policy_rules
WHERE decision = 'allow' AND max_effect = '' AND note = 'remembered from approval';
