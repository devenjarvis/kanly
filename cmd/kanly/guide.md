# Mutation-testing Go changes with Kanly

Coverage says which lines a test runs. Kanly says whether the tests would notice if those lines were wrong: it makes small changes (mutants) to the code, runs the tests against each, and reports the ones no test catches. Use it whenever you write or change Go code or its tests, before calling the work done.

## Loop

1. Run Kanly on what you changed and read the brief it prints:

       {{command}} --diff --diff-base={{base}} --format=llm

   To focus on one function instead: {{command}} --format=llm ./path/to/pkg:FuncName
2. Decide each mutant in the brief one of three ways:
   - **Test it.** Write or sharpen a test so the original code and the mutant behave differently through the package's API.
   - **Simplify the code.** A mutant nothing can observe often marks dead or redundant code. Delete it and the mutant goes with it.
   - **Accept it.** If no input tells the mutant apart from the original, or only a contrived test could kill it, append `<fp> <reason>` to `.kanlyignore` in the package's directory.
3. Re-run only those mutants with the `--fail-fast --mutant=<ids>` command at the end of the brief. Repeat until nothing is left to decide.

## Rules

- Test behaviour a caller can observe. Do not pin full error messages, log text, unexported fields or call order just to kill a mutant: a test that breaks on a harmless refactor costs more than a surviving mutant.
- Never accept a mutant to get past a score threshold. The reason must say why this mutant cannot or should not be killed, specifically enough that a reviewer can check it. "Not worth testing" is not a reason.
- Do not delete a test because it killed no mutants. Most such tests cover code Kanly has no operator for.
- An uncovered mutant means no test in its package runs the line. Tests in other packages do not count.
