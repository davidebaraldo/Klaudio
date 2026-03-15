-- Default team templates seeded on first run.
-- Users can edit or delete these via the API / UI.

INSERT OR IGNORE INTO team_templates (id, name, description, max_agents, review, roles, mode, is_default, created_at, updated_at)
VALUES
(
    'solo',
    'Solo',
    'Single agent handles the entire task. Best for small, focused work.',
    1,
    false,
    '[{"name":"developer","description":"Full-stack developer that implements the entire task","prompt_hint":"You are the sole developer. Implement the task completely, including tests if appropriate.","max_instances":1,"run_last":false}]',
    'sequential',
    true,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
),
(
    'dev-review',
    'Dev + Review',
    'One developer implements, then a reviewer checks quality. Good for production code.',
    2,
    true,
    '[{"name":"developer","description":"Implements the requested changes with tests","prompt_hint":"Implement the task thoroughly. Write clean, well-tested code. Another agent will review your work.","max_instances":1,"run_last":false},{"name":"reviewer","description":"Reviews code for correctness, style, security, and test coverage","prompt_hint":"Review the code changes critically. Check for bugs, security issues, edge cases, and adherence to project conventions. Suggest concrete improvements.","max_instances":1,"run_last":true}]',
    'sequential',
    true,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
),
(
    'full-team',
    'Full Team',
    'Collaborative team with a manager coordinating multiple developers and a reviewer. Best for large, multi-file tasks.',
    5,
    true,
    '[{"name":"manager","description":"Coordinates the team, splits work, resolves conflicts","prompt_hint":"You are the team manager. Coordinate workers via messaging. Monitor progress, resolve merge conflicts, and ensure all pieces integrate correctly.","max_instances":1,"run_last":false},{"name":"developer","description":"Implements assigned subtask in parallel with other developers","prompt_hint":"You are one of several developers working in parallel. Follow the manager''s directives. Communicate progress and blockers via messaging. Avoid modifying files assigned to other workers.","max_instances":3,"run_last":false},{"name":"reviewer","description":"Reviews the integrated result after all developers finish","prompt_hint":"Review all changes made by the team. Check for integration issues, inconsistencies between modules, and overall code quality.","max_instances":1,"run_last":true}]',
    'collaborative',
    true,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
),
(
    'parallel-devs',
    'Parallel Developers',
    'Multiple developers work simultaneously on independent subtasks. No manager overhead — best when subtasks are clearly separable.',
    3,
    false,
    '[{"name":"developer","description":"Implements an independent subtask in parallel","prompt_hint":"You are working in parallel with other developers on separate parts of the codebase. Focus on your assigned subtask. Avoid modifying files outside your scope.","max_instances":3,"run_last":false}]',
    'collaborative',
    false,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
),
(
    'dev-test',
    'Dev + Test',
    'One agent implements, another writes or runs tests. Ensures separation between implementation and verification.',
    2,
    false,
    '[{"name":"developer","description":"Implements the requested feature or fix","prompt_hint":"Implement the task. Focus on the production code. A separate agent will handle testing.","max_instances":1,"run_last":false},{"name":"tester","description":"Writes and runs tests for the implementation","prompt_hint":"Write comprehensive tests for the changes made by the developer. Cover edge cases, error paths, and integration scenarios. Run the tests and report results.","max_instances":1,"run_last":true}]',
    'sequential',
    false,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
);
