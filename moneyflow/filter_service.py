from typing import TYPE_CHECKING

import polars as pl

if TYPE_CHECKING:
    from moneyflow.state import AppState


class FilterService:
    @staticmethod
    def apply_filters(df: pl.DataFrame, state: "AppState") -> pl.DataFrame:
        """
        Apply multiple filters to the DataFrame based on current AppState.
        """
        # Handle empty DataFrame (0 transactions) - return early to avoid column errors
        if len(df) == 0:
            return df

        # Apply time filter
        if state.start_date and state.end_date:
            df = df.filter(
                (pl.col("date") >= state.start_date) & (pl.col("date") <= state.end_date)
            )

        # Apply search filter
        if state.search_query:
            query = state.search_query.lower()
            # Include matches from merchant, category, or Amazon item names
            search_filter = pl.col("merchant").str.to_lowercase().str.contains(query) | pl.col(
                "category"
            ).str.to_lowercase().str.contains(query)
            # Also include transactions matching Amazon item search
            if state.amazon_search_ids:
                search_filter = search_filter | pl.col("id").is_in(list(state.amazon_search_ids))
            df = df.filter(search_filter)

        # Apply group filter (hide Transfers unless enabled)
        if not state.show_transfers:
            df = df.filter(pl.col("group") != "Transfers")

        # Apply hidden filter ONLY for aggregate views
        # Detail views should always show hidden transactions so users can review them
        from moneyflow.state import ViewMode

        if not state.show_hidden and state.view_mode != ViewMode.DETAIL:
            df = df.filter(~pl.col("hideFromReports"))

        # Apply time period drill-down filter (can combine with other dimensions)
        if state.selected_time_year is not None:
            df = df.filter(pl.col("date").dt.year() == state.selected_time_year)

            if state.selected_time_month is not None:
                df = df.filter(pl.col("date").dt.month() == state.selected_time_month)

                if state.selected_time_day is not None:
                    df = df.filter(pl.col("date").dt.day() == state.selected_time_day)

        # Apply view-specific filters (can have multiple levels in multi-level drill-down)
        if state.view_mode == ViewMode.DETAIL:
            if state.selected_merchant:
                df = df.filter(pl.col("merchant") == state.selected_merchant)
            if state.selected_category:
                df = df.filter(pl.col("category") == state.selected_category)
            if state.selected_group:
                df = df.filter(pl.col("group") == state.selected_group)
            if state.selected_account:
                df = df.filter(pl.col("account") == state.selected_account)

        return df
