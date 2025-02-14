<script lang="ts">
  import { copyToClipboard } from "@rilldata/web-common/lib/actions/shift-click-action";
  import {
    useRuntimeServiceGetDescriptiveStatistics,
    useRuntimeServiceGetRugHistogram,
  } from "@rilldata/web-common/runtime-client";
  import { httpRequestQueue } from "@rilldata/web-common/runtime-client/http-client";
  import { runtimeStore } from "@rilldata/web-local/lib/application-state-stores/application-store";
  import { getPriorityForColumn } from "@rilldata/web-local/lib/http-request-queue/priorities";
  import { derived } from "svelte/store";
  import ColumnProfileIcon from "../ColumnProfileIcon.svelte";
  import ProfileContainer from "../ProfileContainer.svelte";
  import {
    getNullPercentage,
    getNumericHistogram,
    getTopK,
    isFetching,
  } from "../queries";
  import NumericPlot from "./details/NumericPlot.svelte";
  import NullPercentageSpark from "./sparks/NullPercentageSpark.svelte";
  import NumericSpark from "./sparks/NumericSpark.svelte";

  export let columnName: string;
  export let objectName: string;
  export let type: string;
  export let mode = "summaries";
  export let example: any;

  export let hideRight = false;
  export let compact = false;
  export let hideNullPercentage = false;

  let active = false;

  $: nulls = getNullPercentage(
    $runtimeStore?.instanceId,
    objectName,
    columnName
  );

  $: numericHistogram = getNumericHistogram(
    $runtimeStore?.instanceId,
    objectName,
    columnName,
    active
  );
  $: rug = useRuntimeServiceGetRugHistogram(
    $runtimeStore?.instanceId,
    objectName,
    { columnName, priority: getPriorityForColumn("rug-histogram", active) },
    {
      query: {
        select($query) {
          return $query?.numericSummary?.numericOutliers?.outliers;
        },
      },
    }
  );
  $: topK = getTopK($runtimeStore?.instanceId, objectName, columnName);

  $: summary = derived(
    useRuntimeServiceGetDescriptiveStatistics(
      $runtimeStore?.instanceId,
      objectName,
      {
        columnName: columnName,
        priority: getPriorityForColumn("descriptive-statistics", active),
      }
    ),
    ($query) => {
      return $query?.data?.numericSummary?.numericStatistics;
    }
  );

  function toggleColumnProfile() {
    active = !active;
    httpRequestQueue.prioritiseColumn(objectName, columnName, active);
  }

  $: fetchingSummaries = isFetching($nulls, $numericHistogram);
</script>

<ProfileContainer
  isFetching={fetchingSummaries}
  {active}
  {compact}
  emphasize={active}
  {example}
  {hideNullPercentage}
  {hideRight}
  {mode}
  on:select={toggleColumnProfile}
  on:shift-click={() =>
    copyToClipboard(columnName, `copied ${columnName} to clipboard`)}
  {type}
>
  <ColumnProfileIcon slot="icon" isFetching={fetchingSummaries} {type} />

  <svelte:fragment slot="left">{columnName}</svelte:fragment>
  <NumericSpark {compact} data={$numericHistogram?.data} slot="summary" />
  <NullPercentageSpark
    nullCount={$nulls?.nullCount}
    slot="nullity"
    totalRows={$nulls?.totalRows}
    {type}
  />
  <div class="pl-10 pr-4 py-4" slot="details">
    <NumericPlot
      data={$numericHistogram.data}
      rug={$rug?.data}
      summary={$summary}
      topK={$topK}
      totalRows={$nulls?.totalRows}
      {type}
    />
  </div>
</ProfileContainer>
