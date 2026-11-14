import { uuid4 } from "./util";
export function templateCells(type) {
  if (type === "blank") return [];
  const common = { uuid: uuid4(), attributes: { x: 0, y: 0, w: 36, h: 10 } };
  if (type === "feed")
    return [
      {
        ...common,
        type: "entityFeed",
        title: "News feed",
        filter: {
          data_type: "composite",
          key: "uri",
          title_key: "features.title",
          query: {},
          options: { preload: true },
        },
      },
    ];
  if (type === "metric")
    return [
      {
        ...common,
        type: "metric",
        title: "Fetch activity",
        filter: {
          data_type: "metric",
          query: { key: "httpFetcher.message.in" },
          options: { preload: true },
        },
      },
    ];
  return [
    {
      ...common,
      type: "chart",
      title: "Title sentiment",
      filter: {
        data_type: "composite",
        key: "features.sentiment.binary_naive_bayes_title",
        fields: ["timestamp", "features.sentiment.binary_naive_bayes_title"],
        query: {},
        agg: {
          agg_name: "avg",
          time_field: "timestamp",
          agg_field: "features.sentiment.binary_naive_bayes_title",
          time_window_seconds: 1800,
        },
        options: { preload: true },
      },
    },
  ];
}
