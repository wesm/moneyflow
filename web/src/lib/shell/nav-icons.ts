import CalendarDaysIcon from '@lucide/svelte/icons/calendar-days'
import CopyIcon from '@lucide/svelte/icons/copy'
import DownloadIcon from '@lucide/svelte/icons/download'
import LandmarkIcon from '@lucide/svelte/icons/landmark'
import LayersIcon from '@lucide/svelte/icons/layers'
import ListIcon from '@lucide/svelte/icons/list'
import PackageOpenIcon from '@lucide/svelte/icons/package-open'
import StoreIcon from '@lucide/svelte/icons/store'
import TagIcon from '@lucide/svelte/icons/tag'
import TagsIcon from '@lucide/svelte/icons/tags'

/** Icons for the navigation entries, keyed by `NavItem.id`. */
export const navIcons: Record<string, typeof StoreIcon> = {
  merchant: StoreIcon,
  category: TagIcon,
  group: LayersIcon,
  account: LandmarkIcon,
  time: CalendarDaysIcon,
  detail: ListIcon,
  duplicates: CopyIcon,
  export: DownloadIcon,
  taxonomy: TagsIcon,
  amazon: PackageOpenIcon,
}
