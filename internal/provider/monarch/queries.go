package monarch

const getSubscriptionDetailsQuery = `
query GetSubscriptionDetails {
  subscription { id }
}`

const getAccountsQuery = `
query GetAccounts {
  accounts {
    id
    displayName
    isHidden
    hideFromList
    deactivatedAt
  }
}`

const getMerchantsQuery = `
query GetAllMerchants {
  byMerchant: aggregates(groupBy: ["merchant"]) {
    groupBy { merchant { id name } }
  }
}`

const getCategoryGroupsQuery = `
query ManageGetCategoryGroups {
  categoryGroups { id name }
}`

const getCategoriesQuery = `
query GetCategories {
  categories { id name group { id } }
}`

const getTransactionsQuery = `
query GetTransactionsList(
  $offset: Int,
  $limit: Int,
  $filters: TransactionFilterInput,
  $orderBy: TransactionOrdering
) {
  allTransactions(filters: $filters) {
    totalCount
    results(offset: $offset, limit: $limit, orderBy: $orderBy) {
      id
      amount
      pending
      date
      hideFromReports
      notes
      category { id }
      merchant { id name }
      account { id displayName }
    }
  }
}`

const updateTransactionQuery = `
mutation Web_TransactionDrawerUpdateTransaction($input: UpdateTransactionMutationInput!) {
  updateTransaction(input: $input) {
    transaction {
      id
      merchant { id name }
      category { id }
      hideFromReports
    }
    errors {
      fieldErrors { field messages }
      message
      code
    }
  }
}`

const deleteTransactionQuery = `
mutation Common_DeleteTransactionMutation($input: DeleteTransactionMutationInput!) {
  deleteTransaction(input: $input) {
    deleted
    errors {
      fieldErrors { field messages }
      message
      code
    }
  }
}`
