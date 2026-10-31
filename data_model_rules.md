---
format: 3
---
Source: Mixed Model Arts (Book 1), Joe Reis, 2026
Scope:  Data modeling across five data forms (structured, semi-structured, unstructured, ML/AI artifacts, metadata) and six architectural layers (transactional, event, analytical, unstructured, ML/AI, knowledge); relational/OLTP schemas, dimensional and wide analytical tables, JSON documents and event streams, feature tables, embeddings, RAG corpora, taxonomies, ontologies and semantic layers consumed by both humans and AI agents; illustrated mainly with SQL (PostgreSQL dialect) and JSON; also the modeling process — requirements, conceptual/logical/physical levels, stakeholder alignment, ownership and change management
Limitations: Book 1 deliberately defers technique depth to later books, so these rules do not specify dimensional modeling mechanics (SCD types, conformed dimensions, bus matrix), Data Vault, normal-form theory, index or partition tuning, query optimisation, or specific vendor platforms; the source is "intentionally light on code" and several of its own examples are internally inconsistent (its bitemporal price table, its open-ended `valid_to` convention — `9999-12-31` in some places, `NULL` in others — and its first-contact-resolution denominator), so the rules state the principle rather than copying those examples; the source never settles how to choose between a centrally governed shared entity and per-context models beyond "shared nouns central, domain metrics local", never gives a threshold for when a modeling pause is justified versus when to ship, and gives no guidance on security, privacy or retention modeling beyond asking whether data should be captured at all; its advice assumes organisations with multiple teams consuming shared data — a single-application team with one consumer will find many [ARCH] rules unnecessary

§1.1  [MUST][METHOD]  Before writing DDL, be able to answer three questions — what is being modeled (conceptual), how it should be structured (logical: entities, attributes, keys, cardinality), and how it will be implemented (physical) — and in a brownfield system you own, reverse-engineer the answers from the physical schema before changing it (for legacy or third-party schemas you do not control, apply §3.5 instead). The levels are tools, not a mandatory sequence of artifacts: you may skip a formal conceptual model in a narrow domain whose concepts are obvious and uncontested, merge logical and physical on a single platform the team knows deeply (accepting the migration lock-in), and ignore the levels while exploring data. Invest in an explicit conceptual model when several teams consume the data, when terms are disputed, or when the domain is complex.
      A team that jumps from vague ideas to physical tables returns to logical modeling anyway while reworking a poor implementation, as when line items were packed into a `cart_payload JSONB` column because the frontend was fast and analysts then had to unpack it on every query.
      ✗  Turn a cross-team stakeholder request directly into `CREATE TABLE` statements and an ORM model without anyone being able to say what the entities mean or how they relate
      ✓  For a cross-team model, agree entities and definitions with stakeholders, define keys and cardinality, and choose types, constraints and indexes for the target platform; for a minor feature in a well-documented system, answer the three questions informally and go straight to a combined logical and physical design
      ↦  §Closing Thoughts: Data Models Are Living Documentation: "If you can’t confidently answer all three, you’re not ready to build."

§1.2  [MUST][METHOD]  Before modeling any core entity, write a plain-language definition that all its stakeholders accept and record who owns it and who has authority to settle disputes about it; when stakeholders' definitions conflict, do not let the data team pick the winner — document each definition with its effect on the numbers and escalate the choice to someone with authority over both parties.
      Without an owned definition, "how many customers do we have?" gets three different numbers (the source's Customer 360 project lost eight months to definition meetings before being cancelled), and a definition chosen by the data team is not owned by the business, so the losing side keeps its own spreadsheet and the "agreed" metric quietly diverges again in production.
      ✗  The analytics engineer starts the `customer` model and decides "active user" means 30-day login because it is easiest to compute
      ✓  Present "30-day login" and "7-day purchase" with their effect on the count to the VP who owns both teams, then record the glossary entry: "Active user — …; owner: Product; disputes escalate to the VP"
      ↦  §The Most Expensive Question in Data Modeling: “What Is a Customer?”: "If you can’t write down what an entity means in plain language that all stakeholders accept, you’re not ready to model it."

§1.3  [MUST][METHOD]  Model the business process before the systems: start from the business question and the process (trigger event, terminating outcome, business object, actors, steps), and choose the technique and platform last.
      Choosing the storage engine or methodology first stretches or amputates business logic to fit it, which produced a technically flawless lakehouse whose numbers nobody could trust.
      ✗  "We're on a columnar warehouse, so let's model this as Kimball" before anyone has described the returns process
      ✓  "Returns start when a customer requests an RMA and end at refund or rejection; actors are customer, warehouse, finance" — then pick the model shape
      ↦  §We Model the Business, Not Just Data Systems: "The model comes first, and the systems that serve it come last."

§1.4  [MUST][METHOD]  Interview for exceptions and the unhappy path, and verify documented processes against how work is actually done (spreadsheets, workarounds, shadow tools) before encoding them in the model.
      A model built only from the documented happy path is wrong the first time information is missing or a system is down, and domain experts reject it with "wait, that's not what happens".
      ✗  Model order fulfilment from the process wiki alone
      ✓  Ask "what do you do when the system is down?" and find the `URGENT_ORDERS` spreadsheet the warehouse actually works from
      ↦  §The Tacit Knowledge Gap: "You must interview for the exceptions."

§1.5  [MUST][METHOD]  Do not adopt an AI-generated schema, join or entity list until a practitioner has checked it against a conceptual model built from stakeholder context; use AI for mechanical translation between levels, not for deciding correctness.
      A generic generated schema cannot know the business's distinctions — carts versus orders, customers versus accounts, subscription pricing — so it builds the wrong thing quickly, and plausible joins on the wrong key produce Cartesian fan-out nobody catches.
      ✗  Prompt "Create a database schema for an e-commerce company" and ship the seven generic tables it returns
      ✓  Give the AI the agreed conceptual model and ask it to draft DDL, then review every join key (`order_id`, not `return_id`) and grain
      ↦  §What Happens When AI Generates Your Schema?: "it cannot produce a correct schema without the conceptual model and context that define your business needs."

§1.6  [MUST][METHOD]  Do not add entities for hypothetical future scenarios or normalize beyond what current use cases require; model today's known needs and evolve the model when needs change. This limits speculative structure, not capture: still record business events at the finest practical grain (§7.5) and timestamps at the finest precision the source provides (§9.8).
      Over-modeling is as damaging as under-modeling: the source's 17-join address-and-translation schema overloaded the CPU and drove developers to abandon it for an unmodeled store.
      ✗  `Street`, `City`, `State`, `Country`, `PostalCode` as five entities with surrogate keys and audit trails, plus a 47-language product-name table, for a single-country launch
      ✓  An `address` entity with those attributes as columns, split further only when a real requirement appears
      ↦  §Level 2: Logical Data Modeling (LDM): "Don’t over-model your data to anticipate every hypothetical future scenario."

§1.7  [MUST][METHOD]  Treat scratchpad models (sandbox schemas, notebooks, ad hoc views) as exempt from formal modeling only until they will run again; when a model is promoted to recurring use, harden it — refactor the SQL, standardise names, make keys robust, add data-quality and freshness tests, and add catalog metadata.
      "Temporary" SQL quietly becomes the company's backbone, and a critical dashboard then breaks on an untested, undocumented view nobody owns.
      ✗  `vw_ops_returns_tuesday_v2`, a 150-line `SELECT` with ad hoc `CASE WHEN` logic, feeding the weekly executive dashboard
      ✓  `fct_product_returns` with `dim_product`, `dim_return_reason` and `dim_date`, surrogate keys, tests, an owner and column descriptions
      ↦  §The Downsides: Hitting the “Modeling Debt Ceiling”: "You can move fast, but you must pay the “refactoring tax.”"

§1.8  [SHOULD][METHOD]  Keep the conceptual and logical models technology-agnostic — no data types, storage engines, indexes, file formats or platform syntax above the physical level — and choose the physical format (Parquet, JSON, Avro) only afterwards, justified by workload constraints such as compression and read- versus write-heavy access.
      Platform details above the physical level lock the design to one engine, so a migration has no abstraction to migrate from, and treating the file format as the model lets storage concerns dictate entities and relationships.
      ✗  A conceptual diagram whose `Customer` box lists `VARCHAR(255)` columns and a `GIN` index; "Our model is Parquet"
      ✓  Conceptual: `Customer` places `Order`; logical: `CustomerID` (PK), `order_line` entity; physical: `customer_id BIGINT`, `order_line` stored as Parquet because the workload is scan-heavy
      ↦  §Level 2: Logical Data Modeling (LDM): "Like the CDM, an LDM is technology- and implementation-agnostic."

§1.9  [SHOULD][METHOD]  Derive the physical design from the logical design, and record every deviation and every modeling shortcut — with the debt it incurs (technical, data or organisational) and when it will be repaid — as a visible backlog.
      When the physical schema silently becomes the model, its accidents become permanent and no one knows they were deviations; unrecorded shortcuts compound, as when a JSON-column shortcut became a fragile ETL, three wrong quarterly revenue dashboards, and a VP who lost credibility.
      ✗  Logical model reverse-fitted to whatever the ORM generated; the catalog stored as one JSON column "for now" with no note
      ✓  PDM notes / ADR: "`OrderItem` has no physical table yet; catalog in JSON for launch; analytics extract is fragile; shred `category` and `price` before Q2 reporting"
      ↦  §Level 3: Physical Data Modeling (PDM): "“The physical design should be derived from the logical design and not the other way around.”"

§1.10  [SHOULD][METHOD]  Publish an explicit out-of-scope list (topics, entities, use cases) at the start of a modeling effort and point to it when new requests arrive.
      Without declared exclusions, every request expands the model until it collapses under its own weight.
      ✗  A returns model that silently grows to cover multi-currency, sub-second latency and churn prediction
      ✓  "Out of scope: sub-second queries; non-USD reporting (Phase 2); predictive churn (separate initiative)"
      ↦  §Declaring What Is “Out of Scope”: "state up front which topics, entities, and use cases are out of scope."

§1.11  [SHOULD][METHOD]  Write the model's business questions as competency questions and test that each one translates into a simple query against the model that returns real results.
      A model that cannot answer its own motivating questions with simple queries has the wrong entities or grain, and the error surfaces only after consumers depend on it.
      ✗  Declare the model done when the ERD is approved
      ✓  "Which product categories had the highest return rate last quarter?" runs as a two-table query and matches finance's figure
      ↦  §Testing the Model: "The business questions should be expressible as simple queries against the conceptual model."

§1.12  [SHOULD][METHOD]  Give every model a change process that names who approves breaking changes, which upstream owners and downstream consumers are consulted and notified, and where changelogs and deprecations are published.
      Without it, an organisational change such as a territory reorg changes what `region` means and silently breaks dashboards, an executive report and an ML model.
      ✗  Rename `region` values in place after the reorg
      ✓  Proposal approved by the model owner, consumers notified, old values mapped and deprecated on a published date
      ↦  §Change Management and Organizational Drift: "Who is the ultimate decision-maker when a breaking change is proposed?"

§2.1  [MUST][ARCH]  When stakeholders use one label for different concepts, model each concept separately under an explicit name — down to the column level, where each domain's meaning gets its own domain-named column recording which actor and domain produced the value, never one flattened generic column; when they use different labels for the same concept, designate one preferred term and record the others as mapped synonyms.
      Merging distinct concepts inflates counts (the source's supply chain nearly ordered inventory from a "customer" table full of prospects) and loses which rule produced a value (one department's "on-time arrival" silently feeds another's compensation calculation), while unmapped synonyms make every query and every LLM guess whether `client` and `customer` are the same.
      ✗  One `customers` table holding everyone who gave an email, buyers distinguished only by a status flag; a single `arrival_timestamp`
      ✓  Separate `registered_account` and `purchasing_customer`; `wheels_down_timestamp` (flight ops), `gate_door_open_timestamp` (customer compensation), `scheduled_arrival_timestamp` (shared); "customer" as preferred term with client/account holder as synonyms
      ↦  §The Most Expensive Question in Data Modeling: “What Is a Customer?”: "Your data model needs to account for the possibility that there might be more than one definition for something like Customer."

§2.2  [MUST][ARCH]  Before joining sources or referencing another domain's entity, reconcile the conceptual definition of each shared term across both owners; each domain may then keep its own logical and physical model, and every boundary crossing must model the translation explicitly rather than relying on a single global table built for every context. Hierarchical classifications are the exception and are shared outright (§2.3).
      Joining a CRM "customer" (anyone with an account) to a support "customer" (anyone who filed a ticket) produces rates that mean neither thing, and a global entity built to satisfy every context grows to hundreds of mostly-null columns.
      ✗  `crm.customers JOIN support.tickets USING (customer_id)` with no definitional check; one `Customer` table carrying lead score, credit score, tax ID and loading-dock number
      ✓  Agree that "tickets per customer" covers account holders only and filter non-account filers before the join; Sales, Fulfillment and Finance each model their own view of the shared `Customer`, with an explicit mapping where they meet
      ↦  §The Levels as Contracts for Data: "both teams must first agree on the conceptual definition of “Customer.”"

§2.3  [SHOULD][ARCH]  Define each hierarchical classification (geography, product category) once as a shared taxonomy whose ordering is strict — no node may sit above its own ancestor — and that all domains use, and constrain classification fields to the taxonomy's approved nodes rather than accepting free text.
      Per-team hierarchies cross wires — nobody knows whether "Pacific" includes Alaska — an inverted parent-child link breaks every roll-up, and free-text categories accept junk ("Sandwich" under Electronics) and cannot be rolled up.
      ✗  Sales `Region = 'West'`, marketing `Area = 'Pacific'`, finance `Territory = 'CA, OR, WA'`; `category = 'Technical Issue'`
      ✓  One `Geography` taxonomy (`categories(category_id, parent_id)`) in which "Pacific" is a child of "West"; `category_id` referencing Technical Issue > Software
      ↦  §Taxonomies: Organizing Concepts into Hierarchies: "you create a single “Geography” taxonomy that everyone must use"

§2.4  [MUST][ARCH]  Ground every AI agent that queries the data in the organisation's own definitions — explicit metadata, a semantic layer and business rules that override the model's general priors — and supply it with the targeted slice of that semantic model relevant to the request, not the whole enterprise model.
      An ungrounded agent asked for "top customers in Utah" reports 5,000 newsletter subscribers as paying clients at production scale, while dumping the entire enterprise model into the context window buries the relevant definitions and exhausts the context.
      ✗  Give the agent raw schema access, or the full enterprise data model, with every question
      ✓  The semantic layer defines `Customer` as having at least one `CompletedOrder`; for a returns question, retrieve only the `Order`, `Return` and `Product` definitions and their relationships
      ↦  §Ontologies, AI Agents, and Semantic Grounding: "Semantic grounding is no longer a nice-to-have but a critical practice"

§2.5  [SHOULD][ARCH]  Give every published table and column business metadata — description including units and currency, owner, and validation status — stored in a catalog, not only technical metadata.
      A name and a type say nothing about meaning: `c_3 DECIMAL(10,2) = 150.75` could be dollars, kilograms or metres, and an LLM will guess.
      ✗  `c_3 DECIMAL(10,2)` with no description
      ✓  `sale_amount_usd`: "Total value of the sale including tax and shipping, in USD"; owner: Finance; status: Validated
      ↦  §Metadata: The Glue That Holds This Together: "It tells us the data’s structure but not its meaning."

§2.6  [SHOULD][ARCH]  Define each business metric once, in a semantic layer or equivalent, with its exact calculation, scope and exclusions, and have every dashboard and agent reuse that definition.
      A metric re-derived in each query drifts between dashboards, and an agent without stated exclusions applies it to cases it was never meant for.
      ✗  `average_order_value` recomputed differently in five dashboards
      ✓  One `return_rate` definition with the description "physical product returns only; not for digital subscription cancellations"
      ↦  §The Semantic Layer Question: "lets analysts reuse definitions without hardcoding them, which is error-prone"

§2.7  [SHOULD][METHOD]  In regulated or standardised domains, look for an existing vocabulary or ontology (Schema.org, SNOMED CT) before inventing one.
      An invented vocabulary must be mapped to the standard later anyway, and until then it cannot interoperate with partners or regulators.
      ✗  A home-grown diagnosis code list in a healthcare model
      ✓  Diagnoses coded to SNOMED CT, with local extensions only where it has gaps
      ↦  §Ontologies: Giving Flexible Structure to Meaning: "start by looking for an existing standard before inventing your own"

§2.8  [CONSIDER][ARCH]  For data that agents can act on, encode the permitted operations (read, aggregate, write, delete, trigger), valid state transitions and policy rules as data rather than leaving them implicit.
      An implicit rule such as "accounts become inactive only after 90 days" is invisible to an agent, which will not ask before violating it.
      ↦  §Action: When Meaning Meets Consequence: "The agent doesn’t know it, and it won’t stop to ask."

§3.1  [MUST][ARCH]  Model each workflow action as a discrete event recording who, what and when, and keep both the business object's current state and its state history.
      A current-state column alone is a compressed event log that discards how the object got there, so process questions and audits are unanswerable.
      ✗  `orders.status = 'Shipped'` updated in place
      ✓  `orders.status` plus `order_events(order_id, event_type, actor_id, occurred_at)`
      ↦  §From Process Flow to Data Model: "The data model must capture not only the current state (e.g., Shipped) but also the history of how it got there."

§3.2  [SHOULD][ARCH]  Normalize transactional system-of-record data by default and serve analytical and ML workloads from separate denormalized models and stores (star schemas, wide tables, feature stores) materialised from it; never use an analytical or denormalized model as the transactional source of truth, never run the warehouse on the OLTP database, and denormalize an operational model only for a stated constraint such as a latency SLA, keeping a separate normalized or analytical model for accuracy and audit.
      Unjustified duplication causes update anomalies and shotgun surgery, one model serving both workloads slows the operational system to a crawl (the source's startup had to cut user functionality), and forcing a normalized model onto a 50 ms fraud check misses the SLA.
      ✗  Customer address copied into `orders`, `invoices` and `shipments` "for convenience"; BI dashboards querying the production PostgreSQL order tables at peak hours
      ✓  Normalized OLTP core with a star schema for BI and a feature store for ML; customer profile denormalized into the fraud event table to meet a 50 ms SLA, with a separate compliance model
      ↦  §Smells across the Five Camps: "In most cases, normalize unless you have a good reason not to."

§3.3  [SHOULD][ARCH]  Build a One Big Table only as a consumption layer fed by a modeled upstream layer, never as a substitute for understanding the source data.
      Raw sources flattened directly into one wide table hide grain and definitional errors, which then surface as silent wrong numbers.
      ✗  Raw CRM, ERP and clickstream dumps joined straight into `obt_customer`
      ✓  Staging, then a modeled core, then `obt_customer` materialised for dashboards
      ↦  §Antiquated and Irrelevant: "using it as an excuse to avoid understanding the underlying data is a recipe for silent errors."

§3.4  [SHOULD][ARCH]  Treat master and reference data as a governed interface: canonical units, explicit hierarchy levels, required fields enforced at write time, and validations that block bad records rather than warn.
      Target Canada's roughly $7 billion failure came from item master data with inconsistent units and identifiers that validations flagged but did not stop.
      ✗  An item master that accepts a SKU with no case-pack quantity and logs a warning
      ✓  Insert rejected until `unit_of_measure`, `case_pack_qty` and `pallet_qty` are present and valid
      ↦  §Target Canada’s \$7 Billion Failure: "Validations that should have blocked flawed SKUs were treated as speed bumps rather than gates."

§3.5  [SHOULD][ARCH]  Do not alter legacy or third-party schemas to fit your model; document how they represent the data and their limits, model downstream of them, and keep upstream server and column names out of your transformations — resolve them through a declared source definition maintained in one place, so an upstream change is absorbed there rather than breaking the model.
      Changing a schema you do not control breaks the system that depends on it and is overwritten by the vendor's next release, and a model with hard-coded dependencies such as server or column names breaks whenever an upstream system changes.
      ✗  `ALTER TABLE` on the ERP's tables to add analytics columns; `SELECT cust_nm, ord_amt FROM prod-db-07.sales.orders` written into each transformation
      ✓  Read the ERP schema, document its quirks, and build the needed model in your own layer from a declared source whose location and upstream-to-model column mapping are defined once in configuration
      ↦  §Legacy and Third-Party Data Models: "Treat legacy data models as museum pieces."

§4.1  [MUST][CODE]  Give every entity instance, in every form of data (rows, JSON documents, files, metadata records such as pipeline runs), a unique identifier that stays fixed for the life of the instance; never use a value that can change (email, phone number, name) as a primary key.
      Without an identifier there is no way to tell which instance you are working with or to link it to anything else, and when an identifier changes everything referencing it breaks — phone numbers are recycled, emails and names change.
      ✗  `pipeline_runs` identified by start time; `customers(email VARCHAR PRIMARY KEY)`
      ✓  `pipeline_runs(run_id UUID PRIMARY KEY, ...)`; `customers(customer_id BIGINT PRIMARY KEY, email VARCHAR UNIQUE)`
      ↦  §Identifiers in Semi-Structured Data: "Every entity instance must have a unique identifier."

§4.2  [MUST][ARCH]  Assume the same real-world entity appears under different identifiers across systems, build identity-resolution logic into the model, and name the owner of the resolution rules and the golden record.
      Unresolved duplicates make a "new customer" in one department a returning customer in another, so figures move depending on who runs them.
      ✗  Count "ACME Corp" and "ACME Corporation, LLC" as two customers
      ✓  A staging layer that resolves source IDs to one `customer_id`, with Sales Ops owning the match rules
      ↦  §Identity Resolution: A Political Problem Disguised as a Technical One: "the resolution logic must be built into your model."

§4.3  [SHOULD][ARCH]  When existing datasets already use different identifiers for the same entity, start with an explicit mapping table rather than refactoring the identifiers in place.
      Refactoring identifiers breaks every dependent system at once; a mapping table can be introduced without disruption, though it must be maintained as sources change.
      ✗  Rewrite `client_id` to `customer_id` across six production databases in one release
      ✓  `customer_id_map(customer_id, source_system, source_id)`
      ↦  §Tying Together Different Forms of Data: "Among these, the mapping table is usually the safest starting point."

§4.4  [SHOULD][ARCH]  Use a system-generated surrogate key when no natural candidate is stable, when integrating sources with conflicting IDs, or when join performance matters, and keep the natural and legacy IDs as indexed attributes; never use a composite natural key such as name plus birth date as a primary key.
      Keying on each source's own ID lets source messiness leak into every foreign key, dropping the natural keys loses human lookup and deduplication, and a composite natural key makes every foreign key multi-column and breaks when two John Smiths share a birth date.
      ✗  Customer data from three legacy systems keyed on whichever system's ID arrived first; `PRIMARY KEY (first_name, last_name, birth_date)`
      ✓  `customer_id` surrogate PK, with `legacy_crm_id`, `erp_id` and `email` as indexed columns; `person_id` surrogate PK with the name and birth-date columns as searchable attributes
      ↦  §When to Use Each Type of Key: "Natural keys are stored as indexed attributes for human lookups and deduplication."

§4.5  [SHOULD][ARCH]  Carry the same persistent entity identifier through every form the entity takes — table rows, event payloads, document and image metadata; where existing datasets already diverge, bridge them with a mapping table (§4.3) rather than rewriting them.
      Without a shared identifier, structured, semi-structured and unstructured data about the same customer cannot be joined.
      ✗  Reviews stored with the reviewer's display name, images keyed by upload filename
      ✓  `customer_id` 101 on the row, in each clickstream event, and in the metadata of every review and image
      ↦  §Tying Together Different Forms of Data: "You should associate each document or text block with that same identifier, typically stored as metadata."

§4.6  [MUST][CODE]  Identify an entity derived from inside a source file (a detected face, a transcript segment) by the source identifier plus its location within the source.
      A file-level key cannot distinguish two detections in the same video or locate the evidence again.
      ✗  `detections(video_filename, label)`
      ✓  `detections(video_id, ts_start, ts_end, bbox, label)`
      ↦  §Identifiers in Unstructured Data: "This key must point to the source video, the precise timestamp range, and the bounding box coordinates."

§4.7  [SHOULD][CODE]  Identify a file entity (image, video, document) by a system-generated UUID, and use a content hash only to identify a version of it.
      Changing one byte changes the hash, so a hash key identifies the file's state rather than the entity and breaks every reference on edit.
      ✗  `images(sha256 PRIMARY KEY, ...)`
      ✓  `images(image_id UUID PRIMARY KEY, content_sha256, ...)`
      ↦  §Identifiers in Unstructured Data: "For the stable entity identifier, rely on a system-generated UUID."

§4.8  [SHOULD][ARCH]  Split any entity that cannot be described in one breath without "and", or that has roughly 30–40 attributes of which three or more apply only to a subset of rows; move each foreign attribute to the entity it describes.
      A God entity accumulates unrelated concerns until the system hits performance problems and every change requires reverse-engineering what the row means.
      ✗  `orders` with `customer_shoe_size`, `marketing_campaign_budget` and `warehouse_manager_last_name`
      ✓  `orders` holding `order_date`, `tax_amount`, `shipping_status`; the other attributes on `customer`, `campaign`, `warehouse`
      ↦  §Attribute Bloat and Cross-Contamination: "If you look at an entity and can’t explain what it represents in one breath without using the word “and,” you are suffering from attribute bloat."

§4.9  [SHOULD][ARCH]  Remove any entity or junction table that domain experts cannot explain.
      Entities created "just in case" carry no business meaning, so nobody can say whether their data is right or what depends on it.
      ✗  `customer_product_xref` that no one can describe
      ✓  Delete it, or name and document the business relationship it records
      ↦  §Common Entity Anti-Patterns: "If domain experts can’t explain what it represents, it probably shouldn’t exist."

§4.10  [SHOULD][ARCH]  Model one concept as one entity — use a discriminator attribute rather than one entity per channel or source of the same thing — and split it across several entities only deliberately (for performance or access control) and with one shared identifier.
      Per-channel entities duplicate structure and force every analysis to `UNION` them back together, and an accidental split scatters overlapping attributes with no way to reconcile which copy is current.
      ✗  `chat_tickets` and `email_tickets`; `CustomerProfile`, `CustomerAccount` and `CustomerRecord` with overlapping columns and no common key
      ✓  `tickets(ticket_id, channel, ...)`; `customer` plus `customer_pii` (restricted access), both keyed by `customer_id`
      ↦  §Common Entity Anti-Patterns: "Splitting is sometimes warranted (for performance or access control), but it should be deliberate, not accidental."

§5.1  [MUST][CODE]  Name every column specifically for what it records; never use reserved words or bare type words such as `date`, `timestamp`, `status` or `type` as column names, and name each time column after its event.
      Reserved words cause syntax errors and constant quoting, and a bare `date` forces every analyst to guess whether it is signup, order or shipment.
      ✗  `tickets(date, timestamp, status)`
      ✓  `tickets(created_at, first_response_at, resolved_at, reopened_at, ticket_status)`
      ↦  §Formatting and Interpreting Time: "Don’t use ambiguous or reserved names like “timestamp” or “date.”"

§5.2  [SHOULD][CODE]  Name entities and columns with the business's own words, apply one naming convention — case, separator, tense, singular or plural — across the whole model, and avoid cryptic names and abbreviations.
      Each technical substitute forces a mental translation on every reader and drifts into errors, and mixed conventions and opaque names slow onboarding until, once the authors leave, nobody knows what `WTF1234` means.
      ✗  `Case` where the business says "Claim"; `OrderDate`, `order_date` and `ORDERDATE` in one schema; a column named `qty_adj2`
      ✓  `claim`, `subscriber`, `order_date`, `order_status`, `adjusted_quantity` throughout
      ↦  §Pay Close Attention to Ubiquitous Language: "If the business calls something a “Claim,” don’t call it a “Case.”"

§5.3  [MUST][CODE]  For every attribute, decide and document whether it is nullable and what `NULL` means in it (missing, unknown, or not applicable), and handle `NULL` explicitly before arithmetic or aggregation over a nullable column — convert it upstream or with `COALESCE` when it means zero.
      An undocumented `NULL` is read differently by each consumer, and because scalar arithmetic propagates `NULL` while `SUM()` and `AVG()` skip it, `AVG(10, NULL, 20)` returns 15 where the business meant 10, silently corrupting results for months.
      ✗  `shipping_address` nullable with no note, empty for digital orders and failed address capture alike; `SELECT AVG(score) FROM reviews` where a missing score means zero
      ✓  `order_type` discriminator with `shipping_address` documented "NULL = not applicable" for digital orders; `SELECT AVG(COALESCE(score, 0)) FROM reviews`
      ↦  §Handling Missing Values: "I’m not a fan of nulls, but because they are pervasive in most databases, you must be intentional about how you handle them."

§5.4  [SHOULD][CODE]  Constrain enumerated attributes to a fixed allowed list validated on input, and when hardening a model, map synonymous source codes onto one canonical value.
      Free entry and unmerged synonyms split one real state or category across several buckets and understate it in every report.
      ✗  `order_status VARCHAR` accepting "shipped", "Shipped" and "SHIPPED "; return reasons `DEFECTIVE`, `DEFECTIVE_PRODUCT` and `DEFECT` reported separately
      ✓  `order_status VARCHAR(50) NOT NULL CHECK (order_status IN ('pending','confirmed','shipped','delivered','cancelled'))`; one `DEFECTIVE` code in `dim_return_reason` with the source variants mapped to it
      ↦  §Attributes in Structured Data: "Ideally, a validation rule checks the input against this allowed list."

§5.5  [SHOULD][CODE]  Replace repeating numbered columns with a child table holding one row per item.
      Numbered columns cap the count, leave readers unable to tell whether they are separate items or process stages, and keep growing.
      ✗  `orders(po_1, po_2, po_3, ... po_10)`
      ✓  `purchase_orders(order_id, po_number, ...)`
      ↦  §Smells across the Five Camps: "Instead of adding another column, consolidate them into rows in a separate table (one row per purchase order)"

§6.1  [MUST][CODE]  Resolve every many-to-many relationship with a junction table holding one row per pair, never with a foreign key on either side or an array that shared or analytical consumers must unpack.
      A foreign key on one side cannot hold the second product of an order, and a JSON array has no referential integrity, must be exploded on every query, and breaks silently when a nested key is renamed.
      ✗  `tickets.product_id`, or `orders.cart_payload JSONB` holding the line items
      ✓  `order_items(order_id REFERENCES orders, product_id REFERENCES products, quantity, unit_price)`
      ↦  §Entities, Attributes, and Relationships: "you need a junction entity—TicketProducts—with one row per ticket-product pair"

§6.2  [MUST][CODE]  Declare referential relationships as foreign-key constraints in the physical model, and make a mandatory parent reference `NOT NULL`.
      Undeclared or nullable references allow orphan rows that no join can attribute, and neither humans nor agents can traverse an undeclared relationship correctly.
      ✗  `orders(customer_id INTEGER)` with no constraint
      ✓  `orders(customer_id INTEGER NOT NULL REFERENCES customers(customer_id))`
      ↦  §One-to-Many (1:N) Relationships: "Orders.CustomerID cannot be null—ensures that every order is associated with a customer. No “orphaned” orders are allowed."

§6.3  [SHOULD][ARCH]  Where the platform does not enforce key and relationship constraints (cloud warehouses, dataframes, spreadsheets), enforce primary-key uniqueness, foreign-key integrity and not-null in the pipeline as tests.
      Declarative-only constraints are not checked, so duplicate keys and orphans enter unnoticed.
      ✗  `PRIMARY KEY (order_id) NOT ENFORCED` in the warehouse and no pipeline test
      ✓  `unique` and `relationships` tests on `order_id` and `customer_id` that fail the load
      ↦  §Constraints and Cardinality: "the responsibility for enforcing them shifts to the data pipeline itself"

§6.4  [SHOULD][ARCH]  Promote a relationship to its own entity, with its own key and attributes, as soon as it needs to store data of its own.
      A bare link cannot record when, where or under what terms the relationship holds, so questions about the relationship itself are unanswerable.
      ✗  `person.spouse_id`
      ✓  `marriages(marriage_id, person_a_id, person_b_id, married_on, location)`
      ↦  §The Knowledge Camp: Meaning and Inference: "a relationship becomes an entity the moment it needs to store its own data."

§6.5  [SHOULD][CODE]  In document models, embed data owned by a single parent by default, but reference it instead when it is shared by more than one parent, changes independently, or grows without a predictable bound; data that shared or analytical consumers read across a many-to-many relationship still follows §6.1.
      Embedding unbounded or shared data makes documents grow without limit and leaves stale copies when the shared data changes.
      ✗  Five years of order history embedded in each customer document; full product records embedded in every order
      ✓  Customer profile embedded in the customer document; products referenced by `product_id` from a shared catalog
      ↦  §Embed vs. Reference: "Switch to referencing when data is shared across more than one parent, changes independently, or grows without a predictable bound."

§6.6  [SHOULD][CODE]  Store transaction-time values such as unit price on the line item rather than deriving them from the current product record.
      Deriving from the current record means historical order values quietly change every time the price is updated.
      ✗  Revenue computed as `order_items.quantity * products.unit_price`
      ✓  `order_items.unit_price DECIMAL(10,2) NOT NULL`, captured at purchase
      ↦  §Phase 2: The Intermediate Design (LDM): "lean on the price in Product alone, and your historical order values quietly change every time someone updates a price."

§6.7  [SHOULD][ARCH]  Mark probabilistic relationships (similarity scores, inferred entity matches, recommendations) as inferred with a confidence and an explicit action threshold, route edge cases to human review, and use only explicit enforced relationships where correctness has real-world consequences.
      Treating an inferred link as fact misclassifies customers and corrupts financial or compliance outputs that assume certainty.
      ✗  Merge two customer records into one invoice because their embedding cosine is 0.94
      ✓  Store the match with `confidence = 0.94`, auto-apply only above an agreed threshold, queue the rest for review, and bill only from the enforced `customer_id`
      ↦  §Inferred Relationships: When You Need Discovery: "Flag them, weight them, and let humans review the edge cases."

§6.8  [CONSIDER][ARCH]  When a use case needs many-hop traversal of a relationship (friend-of-a-friend, deep hierarchies), consider a graph store rather than deep recursive CTEs in a relational database.
      Deep recursive self-joins become hard to maintain and a bottleneck at scale.
      ↦  §Direction and Recursive Relationships: "a relational database may struggle."

§7.1  [MUST][CODE]  Document the grain of every table, dataset and feature set as a one-sentence "one row per …" statement before it ships.
      Consumers who do not know what a row represents join and sum it wrongly, producing fan-out and double counting.
      ✗  A `sales` table with no stated grain
      ✓  "`daily_product_sales`: one row per product per calendar day (UTC)"
      ↦  §Identify and Align the Data Grain: "Every table or dataset must have its grain explicitly documented and understood by all users."

§7.2  [MUST][CODE]  Annotate every join with its effect on grain (preserves, raises, lowers) and verify row counts before and after; aggregate only rows that share one grain, and make any change of grain an explicit operation such as `COUNT(DISTINCT ...)` or a `GROUP BY`, never a side effect of a join (applies especially when a parent has several child relationships: aggregate each child separately at the parent key, then join the aggregates).
      An unannotated one-to-many join silently changes the result to one row per child, so parent-level values are counted once per child — a customer with 3 subscriptions and 5 tickets shows 15 ticket rows — and mixed-grain aggregation produces figures no one can interpret.
      ✗  Sum `signup_bonus` from `customers JOIN orders`; `SELECT c.id, COUNT(t.id) FROM customers c JOIN subscriptions s ON ... JOIN tickets t ON ... GROUP BY c.id`
      ✓  Note "grain becomes one row per order" and take customer-level measures from `customers` alone; CTEs aggregating `tickets` and `subscriptions` by `customer_id`, then `customers LEFT JOIN` each
      ↦  §Fan-Out: "The lesson is always to verify that your join conditions preserve (or intentionally change) the desired grain."

§7.3  [MUST][ARCH]  Make incremental loads and retries idempotent at the table's stated grain.
      A load that crashes halfway and retries duplicates rows at the grain, and the daily revenue figure is double-counted.
      ✗  Append-only `INSERT` of the day's orders on every retry
      ✓  `MERGE` on the grain key, or delete-and-replace of the day's partition
      ↦  §Additivity and Nulls: "This operational trap requires idempotent design or exactly-once processing guarantees"

§7.4  [SHOULD][CODE]  Keep only columns that belong to the table's stated grain, separating headers from lines, and do not mix rows of different grain in one table; if a rollup table must hold several levels, add a `grain_level` column that every query filters on.
      A column from a coarser grain is repeated per line, so a header amount is multiplied when summed, and a plain `SUM()` over mixed-grain rows double-counts revenue.
      ✗  `order_lines` carrying `order_shipping_fee` and `customer_lifetime_value`; `sales` containing both per-purchase rows and daily-total rows
      ✓  `order_header` (one row per order, with `shipping_fee`) and `order_line` (one row per product per order); a rollup with `grain_level IN ('day','month')`
      ↦  §The Grain Audit Checklist (Structured Data): "Any column that pertains to a coarser or finer grain is a sign that the grain has drifted."

§7.5  [SHOULD][ARCH]  Store business events at the lowest practical grain — when in doubt go finer — and aggregate for performance only while keeping the atomic data and the keys that lead back to it; never delete event-level data because a summary exists.
      Coarse data can never be deterministically disaggregated, and a summary without its keys cannot be verified or re-cut, so a drill-down nobody anticipated becomes impossible.
      ✗  Build `weekly_active_users_by_product` and drop the raw events to save storage
      ✓  Store transactions at order-line grain, derive `daily_sales` from them, and keep the events in cold storage with the keys that answer "which orders make up the $1M?"
      ↦  §Preserve Disaggregation Paths: "The rule: aggregate aggressively for performance, but preserve a path back to atomic data."

§7.6  [CONSIDER][ARCH]  Tag each dataset exposed to agents with its grain and the grains it can roll up to, and treat RAG chunk size and stream windows as recorded grain decisions.
      Without grain tags an agent routes a yearly question to a transaction table or a per-order question to a daily summary.
      ↦  §Grain Beyond Structured Data: "A robust semantic layer solves this by explicitly tagging each dataset with its grain"

§8.1  [MUST][METHOD]  Before reporting any count, define what one instance is (account, device or email), what counts as existing or "active", whether the quantity is discrete or continuous, and the time range, definition and source system.
      Undefined counts diverge: the source found "active user" counts varying by a factor of three inside one company.
      ✗  `SELECT COUNT(*) FROM users` reported as "active users"
      ✓  "Active users: distinct `account_id` with at least one login in the trailing 30 days, from the auth service"
      ↦  §What Does It Mean to Count Something?: "Four things must be true before a count has any meaning:"

§8.2  [MUST][CODE]  Treat every many-to-many relationship as an aggregation hazard: before grouping across it, ensure each instance contributes exactly once to a total, or label the groups as overlapping.
      Overlapping groups double count — a $300 smartwatch tagged Electronics, Fitness and Accessories sums to $900.
      ✗  Total revenue computed by summing per-category revenue over `product_categories`
      ✓  Total from the order-line grain; per-category figures labelled as overlapping or allocated
      ↦  §Grouping and Disjointness: "Every time you see a many-to-many relationship, treat it as a red flag for aggregation safety."

§8.3  [MUST][CODE]  Classify every measure as additive, semi-additive or non-additive, and never sum a semi-additive measure such as a balance across time.
      Adding yesterday's balance to today's yields a number with no meaning.
      ✗  `SUM(account_balance)` grouped by month over daily snapshots
      ✓  Sum balances across accounts for one date, or take the end-of-period balance
      ↦  §Additivity and Nulls: "adding a single customer’s balance from yesterday and today yields no meaningful result."

§8.4  [MUST][CODE]  Never re-aggregate a non-decomposable result: store ratios, averages, rates and margins as their additive numerator and denominator and compute the ratio at query time; never add distinct counts across periods or partitions or combine per-partition medians or percentiles — recount from atomic data, compute holistic measures globally, or use an explicit mergeable approximation (HyperLogLog, t-digest) where the business accepts a bounded error.
      Averaging averages weights each group equally regardless of size (store averages of $45, $75 and $80 give $66.67 where the true figure is $49.13), 100 unique users on Monday plus 100 on Tuesday is not 200, and averaging partition medians gives a result that depends on which server ran the query.
      ✗  `AVG(store_avg_order_value)`; a stored `fcr_rate` later averaged by week; `SUM(daily_unique_users)` reported as weekly unique users
      ✓  `SUM(revenue) / SUM(order_count)`; stored `resolved_first_contact_count` and `resolved_count`; `COUNT(DISTINCT user_id)` over the week's events, or a HyperLogLog sketch merged across days
      ↦  §The Average of Averages Trap: "Never aggregate pre-computed averages."

§8.5  [MUST][CODE]  Give every aggregation explicit time and dimensional bounds, and never hard-code a date literal that silently goes stale.
      An unbounded "Total Revenue" card cannot be interpreted, and a filter written as `created_at > '2023-01-01'` still runs three years later as if it were current.
      ✗  "Revenue: $10,000" from `WHERE created_at > '2023-01-01'`
      ✓  "Revenue, Western Region, Q1 2026" with the period passed as a parameter
      ↦  §Boundedness: "Every aggregation must be bounded, even if only implicitly."

§8.6  [MUST][CODE]  In streaming aggregation, never sum results of overlapping (sliding) windows, and define in advance how an event arriving after its window has closed is handled.
      A 5-minute window sliding every minute counts each event five times, and undefined late-event handling makes closed totals silently wrong or inconsistent.
      ✗  Sum sliding-window counts to get an hourly total
      ✓  Tumbling windows for additive totals, with a declared allowed lateness and update rule
      ↦  §Universality of Principles: "If a late event arrives after the window has closed, your model needs a predefined rule for how (or if) to update that aggregation."

§8.7  [MUST][METHOD]  Before reporting a combined rate or trend, check it for Simpson's paradox: compute the same measure separately for each subgroup of every breakdown dimension listed in the metric's definition (§2.6), or of each dimension the figure combines across when no definition exists, and compare each subgroup's direction with the combined result. The check is complete when every such subgroup figure has been computed and any subgroup whose direction is opposite to the combined result is reported alongside it.
      A combined rate can point the opposite way from every subgroup, leading to the opposite decision from the one the data supports.
      ✗  Report that conversion rose overall without checking each channel
      ✓  Report the overall rate alongside per-channel rates and flag any reversal
      ↦  §Preserve Disaggregation Paths: "Always check whether your aggregation obscures contradictory subgroup patterns."

§8.8  [SHOULD][CODE]  Do not average values whose average is meaningless in their domain — identifiers, codes, categories, or embeddings of semantically unrelated content (the source states closure absolutely; narrowed here because ratios and means of integers legitimately leave the input type).
      An "average zip code" is nonsense, and a mean-pooled vector of chunks about indexing, French cooking and marathons matches none of them, so retrieval misses.
      ✗  Mean-pool every chunk of a mixed-topic document into one embedding
      ✓  Pool only chunks that share a semantic neighbourhood, and keep the others separate
      ↦  §Decomposability and Closure: "An aggregate is structurally valid only if it remains within the original domain."

§8.9  [SHOULD][CODE]  Reconcile each key metric by computing it two ways — from the raw data and from the aggregate — and treat any mismatch as a grain bug.
      Grain errors otherwise reach dashboards unnoticed.
      ✗  Ship `monthly_revenue` without comparing it to the transaction total
      ✓  A test asserting `SUM(monthly_revenue)` equals `SUM(order_lines.amount)` for the same period
      ↦  §The Grain Audit Checklist (Structured Data): "Mismatches surface grain bugs immediately."

§9.1  [MUST][CODE]  Model event time, ingestion time and processing time as separate fields, and never use processing or ingestion time in place of event time.
      Late-arriving data placed by processing time lands in the wrong period, and time-based models built on it break.
      ✗  Daily sales bucketed by `loaded_at`
      ✓  `event_time`, `ingested_at` and `processed_at` columns, with business reporting on `event_time`
      ↦  §Fundamental Types of Time: "Don’t confuse processing time with event time."

§9.2  [MUST][CODE]  Store dates and times in native temporal types, never as strings (ISO 8601 for any textual representation), and store raw timestamps as absolute UTC instants with `Z` or an explicit offset; when local time matters, store the IANA zone name in a separate column.
      String dates sort wrongly ("12/01/2024" before "2/15/2024") and are ambiguous ("01/02/2025"), and a floating local timestamp shifts by hours depending on who reads it, with the original zone unrecoverable for daylight-saving ambiguity.
      ✗  `order_date VARCHAR(10) = '01/02/2025'`; `'2025-10-11 14:30:00'` in a `timestamp` column
      ✓  `order_date DATE = '2025-01-02'`; `'2025-10-11T14:30:00Z'` in a `timestamptz` column, plus `event_tz = 'America/Denver'`
      ↦  §The Anatomy of Absolute Time: Why You Need the “Z”: "always store raw timestamps in UTC with the Z (or an explicit offset like -04:00)."

§9.3  [MUST][CODE]  Never write custom time zone conversion logic: use an established time zone library, compute in UTC — including durations — and convert to local time only for presentation; handle open-ended end times explicitly and document whether day counts are inclusive or exclusive.
      Hand-rolled conversion mishandles daylight-saving transitions (01:30 on 2023-11-05 happened twice in Mountain Time), subtracting timestamps in different zones gives misleading durations, and a `NULL` end time makes the duration `NULL`.
      ✗  `local_time = utc_time - INTERVAL '7 hours'`; `resolved_at - created_at` over mixed-zone values with open tickets left `NULL`
      ✓  `utc_time AT TIME ZONE 'America/Denver'` in the presentation query; `COALESCE(resolved_at, CURRENT_TIMESTAMP) - created_at` on UTC values, with "exclusive of end date" documented
      ↦  §Time Zone Complexities: "Don’t write your own time zone conversion logic."

§9.4  [MUST][CODE]  For late-arriving files, extract the embedded event time (such as EXIF), store ingestion time as well, and deduplicate by content hash rather than file name.
      File names differ between copies, so name-based deduplication keeps duplicates and ingestion time misplaces the content in time.
      ✗  Deduplicate synced photos by `filename`
      ✓  `photos(photo_id, captured_at_exif, ingested_at, content_sha256)` with deduplication on `content_sha256`
      ↦  §Late-Arriving Unstructured Data: "You can’t rely on file names, since they might differ."

§9.5  [SHOULD][CODE]  Give facts with a lifespan (address, price, tier, policy documents, knowledge-graph facts) `valid_from` and `valid_to`, write point-in-time lookups as half-open intervals, and filter retrieval for agents by validity at the time asked about.
      A one-sided predicate returns several versions for one date, and retrieval without validity returns a superseded policy that the agent then states confidently and wrongly.
      ✗  `WHERE valid_to >= '2023-02-15'`; the 2022 return policy and its March 2026 replacement indexed identically
      ✓  `WHERE valid_from <= DATE '2023-02-15' AND valid_to > DATE '2023-02-15'`; each policy chunk carries `valid_from`/`valid_to` and retrieval filters to the policy valid on the question's date
      ↦  §Point-In-Time Queries: "Remember, this is not a point-in-time query."

§9.6  [SHOULD][ARCH]  Where you must answer what was known and when (audit, SLA disputes, corrections), use a bitemporal model that keeps both valid time and transaction time and never overwrites the old record.
      Overwriting fixes the current answer but destroys what the system believed at the time, so you cannot tell whether a team missed an SLA or the system misled them.
      ✗  `UPDATE tickets SET priority = 'high'` after triage
      ✓  Close the old row's `txn_to` and insert a new version, keeping both `valid_from`/`valid_to` and `txn_from`/`txn_to`
      ↦  §Bitemporal: "In a bitemporal model, you don’t overwrite the old record."

§9.7  [SHOULD][ARCH]  Model the fiscal calendar once as a date dimension (`calendar_date`, `fiscal_year`, `fiscal_quarter`, `fiscal_month`, `fiscal_week`, `is_fiscal_year_end`) and reuse it everywhere.
      Fiscal periods recomputed in each query produce wrong quarterly reports when the fiscal year does not start in January.
      ✗  `EXTRACT(QUARTER FROM order_date)` for a February fiscal year
      ✓  `JOIN dim_date USING (calendar_date)` and group by `fiscal_quarter`
      ↦  §Model Your Fiscal Calendar: "Build it once, use it everywhere."

§9.8  [SHOULD][CODE]  Capture timestamps at the finest precision the source provides.
      Precision can be truncated later but never recovered, so finer-grained questions become unanswerable.
      ✗  Truncate click events to the minute on ingest
      ✓  Store millisecond `event_time` and truncate in queries
      ↦  §Formatting and Interpreting Time: "you can always truncate to the day later, but you can’t recover milliseconds you didn’t capture."

§10.1  [MUST][ARCH]  Model the operational domain as well as the business domain: datasets, pipeline runs and schemas as entities with lineage, quality scores and access logs; classify each timestamp as either a business-event or a system-processing attribute; and in any dataset that may contain synthetic data, carry lineage, provenance and quality signals that distinguish real from synthetic records.
      Without it, trust and governance are bolted on afterwards, a column such as `last_updated` is read as a business fact when it is a load artefact, and unmarked synthetic records are trained and reported on as if they were observed.
      ✗  `last_updated` on `customers` with no indication whose change it records; synthetic transactions appended to `transactions` with no marker
      ✓  `customer_updated_at` (business) and `_loaded_at` (system), with `pipeline_runs` and lineage records; `is_synthetic`, `generator_version` and `source_dataset_id` on every record
      ↦  §Metadata as a First-Class Attribute: "you don’t just model the business domain; you must also model the operational domain."

§10.2  [SHOULD][CODE]  When ingesting semi-structured data, shred stable, high-value fields into typed columns and keep only the variable remainder as a raw JSON column; give each field that carries a core concept one type and shape across all documents, enforced with a schema (JSON Schema validation or Avro schema evolution) with defined extension points for variable fields.
      Shredding everything forces a pipeline change on every payload change, keeping everything raw turns every query into fragile string manipulation, and inconsistent shapes break every downstream extract each time the application changes.
      ✗  Clickstream stored as a single `payload VARIANT` column; product `category` as an array in some documents, an object in others and a string in the rest
      ✓  Typed `user_id`, `event_time`, `event_type` and `page_url` columns plus a `properties VARIANT` column; a validated core (`sku`, `name`, `price`, `category_id`) plus a schema-defined `attributes` object
      ↦  §Modeling Implications for Semi-Structured Data: "Shred the stable, high-value fields into typed columns."

§10.3  [SHOULD][ARCH]  Store unstructured content in object storage, reference it by URI from a structured metadata record (who, what, when, version) in the relational model, extract attributes from it once in a pipeline into structured columns (schema-on-write) rather than calling an LLM inside a view or query on every read, and keep derived features in their own modeled stores.
      Treating content as tabular fails, derived features stored as ad hoc columns lose their lineage to the source, and per-query extraction is extremely slow and spends compute and token budget on every refresh.
      ✗  Product images as `BYTEA` in the catalog table; a SQL view that calls an LLM function on `review_text` for each query
      ✓  `product_images(image_id, product_id, s3_uri, uploaded_at, is_appropriate, detected_objects)` with embeddings in the vector store; a pipeline writing `sentiment_product` and `keywords` to `review_attributes` when each review lands
      ↦  §Modeling Implications for Unstructured Data: "store a reference (URI) in your relational model alongside metadata"

§10.4  [SHOULD][CODE]  Store extracted sentiment per aspect rather than as one blended score.
      A blended score averages strong opposing signals to near zero and destroys the granularity of the feedback.
      ✗  `overall_sentiment = 0.05`
      ✓  `sentiment_product = 0.9`, `sentiment_shipping = -0.8`
      ↦  §Attributes in Unstructured Data: "This is an anti-pattern because it destroys the granularity of the feedback."

§10.5  [MUST][ARCH]  Build ML training sets at the same grain as the label — one row per thing being predicted — and point-in-time correct, joining each example only to feature values known at or before its observation time.
      Grain-mismatched or future-leaking features skew training against serving: a model predicting last month's churn from today's balance looks brilliant in development and collapses in production.
      ✗  Raw event-level rows, or the current `account_balance`, joined to a per-customer churn label
      ✓  Features aggregated to one row per customer per snapshot date via an `AS OF` join on `observation_time` against the balance history
      ↦  §Bitemporal: "each training example may see only what was known at that moment"

§10.6  [MUST][ARCH]  When an AI agent is an actor in a process, persist its input context — prompt, available tools, model version — alongside each action it takes.
      Without the input context you cannot debug the process when the agent goes wrong.
      ✗  `agent_actions(action_id, order_id, action, created_at)`
      ✓  `agent_actions(..., prompt, tools_available, model_version)`
      ↦  §Capture the Process Flow: "The model must also capture the input context (prompts, available tools, model version, etc.) that led to it."

§10.7  [SHOULD][ARCH]  Model ML and agent artifacts as data with schemas: link each trained model to its training datasets, feature steps, runs and hyperparameters, and store agent tool-call logs and memory as modeled entities.
      Unmodeled artifacts cannot be traced, so a model's or agent's output cannot be explained or reproduced.
      ✗  Agent traces dumped as unstructured log lines
      ✓  `tool_calls(call_id, agent_run_id, tool_name, arguments, result, called_at)` and `model_versions(model_id, training_dataset_id, run_id, hyperparameters)`
      ↦  §Machine Learning/AI Artifacts: "These artifacts are data forms that require modeling."

§10.8  [SHOULD][ARCH]  Track the production distribution of every model feature and alert on drift from its training distribution.
      Untracked drift degrades predictions silently while every pipeline still reports success.
      ✗  Retrain annually with no monitoring of feature distributions
      ✓  Daily distribution statistics per feature, compared to the training snapshot, with an alert threshold
      ↦  §Smells across the Five Camps: "feature drift, where the distributions a model was trained on quietly shift in production, but these changes are somehow untracked."
