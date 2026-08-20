# Salesforce Integration

Syncs accounts and opportunities between our CRM view and Salesforce
production org `acme.my.salesforce.com`.

## Fields sent to Salesforce

- `Account.Name` — from `account.legal_name`
- `Account.External_Id__c` — internal account UUID
- `Account.OwnerId` — mapped from `account.owner_email` via SF user lookup
- `Opportunity.Amount` — from `deal.arr_usd`
- `Opportunity.CloseDate` — from `deal.expected_close`
- `Opportunity.StageName` — mapped from our funnel stages

## Jobs

| Job                | Schedule (UTC) | Description                       |
|--------------------|----------------|-----------------------------------|
| `sfdc-account-sync`  | hourly at :05 | Upserts accounts modified in last 2h |
| `sfdc-opp-sync`      | hourly at :15 | Upserts opportunities                |
| `sfdc-full-refresh`  | Sundays 03:00 | Full reload; runs ~40 min            |
