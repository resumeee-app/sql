<p align="center">
  <img src="./.github/logo-192x192.png" alt="Resumeee" width="96" />
</p>

# sql

SQL persistence backend for **Resumeee** (`github.com/resumeee-app/sql`), implementing the `core` storage interfaces.

## What It Contains

- **Database Drivers** — Pure-Go SQLite (embedded default), PostgreSQL, and MySQL support.
- **Schema & Seeding** — Automatic schema migrations and baseline template/preset seeding.
- **Durable Job Queue & Tenant Isolation** — Persistent storage for profiles, branches, versions, quotas, and background tailoring jobs.

## Verification

Requires **Go 1.27+**.

```bash
GOWORK=off make verify
```

---

## License & Trademarks

### License
Resumeee is licensed under the **GNU Affero General Public License v3.0 or later** ([AGPL-3.0-or-later](LICENSE)).

You are free to use, modify, and host this software. Under the AGPL, any modified versions distributed or hosted over a network must make their complete source code freely available under the same license terms.

For commercial inquiries or custom closed-source licensing arrangements, contact Ruben Rivera.

### Contributor Agreement
All external contributions are welcomed under the terms of our [Contributor License Agreement (CLA)](CLA.md). See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

### Trademark Notice
"Resumeee" and the Resumeee branding and logo are trademarks of Ruben Rivera. The AGPLv3 license gives you rights to the software code, but does not grant rights to use the official name, logo, or trademarks for derivative products or publicity without prior written permission.
