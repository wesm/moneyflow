<script lang="ts">
  import { DetailDrawer } from '@kenn-io/kit-ui'

  import type { TransactionInformationResponse } from '../lib/api/client'

  interface Props {
    information: TransactionInformationResponse
    onclose: () => void
  }

  let { information, onclose }: Props = $props()
  const matches = $derived(information.matches ?? [])
</script>

<DetailDrawer
  title="Transaction information"
  ariaLabel="Transaction information"
  width="min(680px, calc(100vw - 1px))"
  {onclose}
>
  <section class="transaction-information">
    <dl class="editing-summary">
      <div>
        <dt>Date</dt>
        <dd>{information.transaction.date}</dd>
      </div>
      <div>
        <dt>Merchant</dt>
        <dd>{information.transaction.merchant}</dd>
      </div>
      <div>
        <dt>Account</dt>
        <dd>{information.transaction.account}</dd>
      </div>
      <div>
        <dt>Category</dt>
        <dd>{information.transaction.category}</dd>
      </div>
      <div>
        <dt>Amount</dt>
        <dd>{information.transaction.amount.display}</dd>
      </div>
    </dl>
    {#if information.amazon_item}
      <h2>Amazon order item</h2>
      <p>{information.amazon_item.product_name}</p>
      <dl class="editing-summary">
        <div>
          <dt>Order date</dt>
          <dd>{information.transaction.date}</dd>
        </div>
        <div>
          <dt>Quantity</dt>
          <dd>{information.amazon_item.quantity}</dd>
        </div>
        {#if information.amazon_item.order_status}
          <div>
            <dt>Order status</dt>
            <dd>{information.amazon_item.order_status}</dd>
          </div>
        {/if}
        {#if information.amazon_item.shipment_status}
          <div>
            <dt>Shipment</dt>
            <dd>{information.amazon_item.shipment_status}</dd>
          </div>
        {/if}
      </dl>
    {:else if information.amazon_qualified}
      <h2>Possible Amazon orders</h2>
      {#if matches.length === 0}
        <p>No Amazon order-history match was found.</p>
      {:else}
        <ol class="transaction-matches">
          {#each matches as match (`${match.order_id}-${match.class}`)}
            <li>
              <strong>{match.first_product}</strong>
              <span>{match.confidence} · {match.class} · {match.order_total.display}</span>
              {#if (match.items ?? []).length > 1}
                <ul>
                  {#each match.items ?? [] as item (`${item.asin ?? 'asinless'}-${item.product_name}`)}
                    <li>{item.product_name} × {item.quantity}</li>
                  {/each}
                </ul>
              {/if}
            </li>
          {/each}
        </ol>
        {#if information.total_matches > matches.length}
          <p>{information.total_matches - matches.length} more matches are not shown.</p>
        {/if}
      {/if}
    {/if}
  </section>
</DetailDrawer>
