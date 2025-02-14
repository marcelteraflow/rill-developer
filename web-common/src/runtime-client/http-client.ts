import { HttpRequestQueue } from "@rilldata/web-local/lib/http-request-queue/HttpRequestQueue";
import type { RequestQueueEntry } from "@rilldata/web-local/lib/http-request-queue/HttpRequestQueueTypes";
import type { FetchWrapperOptions } from "@rilldata/web-local/lib/util/fetchWrapper";

let RuntimeUrl = "";
try {
  RuntimeUrl = RILL_RUNTIME_URL;
} catch (e) {
  // no-op
}

export const httpRequestQueue = new HttpRequestQueue(RuntimeUrl);

export const httpClient = async <T>(
  config: FetchWrapperOptions
): Promise<T> => {
  return (await httpRequestQueue.add(config)) as Promise<T>;
};

export default httpClient;
