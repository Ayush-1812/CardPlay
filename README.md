# CardPlay planning package

Prepared 29 September 2026. Documentation only; no game implementation was created.

1. [Product requirements](docs/01-product-requirements.md)
2. [Monopoly Deal rule specification](docs/02-monopoly-deal-rules.md)
3. [Reference UI and code review](docs/03-reference-review.md)
4. [Asset and license inventory](docs/04-assets-and-licenses.md)
5. [Roadmap and release acceptance criteria](docs/05-roadmap-and-acceptance.md)

The selected source edition is the US English Hasbro/Parker Brothers Monopoly Deal booklet, product **01723**, instruction code **01723-I**, ©1935, 2008. The PDF was produced in 2009; this does not establish a different retail edition. It is the card game, not the Monopoly board game. The supplied booklet does not identify a later SKU or establish which physical printing the user owns.

The complete one-page foldout and all five pages of the supplementary guide were read. The booklet takes precedence over the supplementary guide, which explicitly describes itself as a fan-site summary. The user-requested monopolydealrules.com booklet/card photographs and FAQ were also inspected. The reference repository is inspiration and review evidence only.

**Rules status:** the website's card photographs close the missing card-data gaps. Remaining interpretation questions Q1–Q3 are recorded in the rule specification. Suggested answers are not approvals. Implementation of those disputed behaviors must wait for resolution; the documentation phase is complete independently of those answers.

All generated project files are under this folder. The original PDFs and reference repository remain outside it and unchanged. `docs/evidence/` contains local research extracts, PDF renders and inventory CSVs, not production assets. Do not bundle that directory into the website.
