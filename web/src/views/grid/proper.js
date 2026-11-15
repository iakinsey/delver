import GridLayout from "react-grid-layout";
import React from "react";
import CrawlLog from "../cell/crawlLog";
import ChartView from "../cell/chart";
import EntityFeedView from "../cell/entity";
import MapView from "../cell/map";
import { getCells, getWindowSize } from "../../event/selectors";
import { updateCell } from "../../event/actions";
import { connect } from "react-redux";
import store from "../../event/store";
import MetricView from "../cell/metric";

const renderCellContents = (cell, dimensions) => {
  const key = cell.uuid;

  switch (cell.type) {
 case "crawlLog":return <CrawlLog key={key} cell={cell}/>;
    case "chart":
      return <ChartView key={key} cell={cell} />;
    case "map":
      return <MapView key={key} cell={cell} />;
    case "entityFeed":
      return <EntityFeedView key={key} cell={cell} />;
    case "metric":
      return <MetricView key={key} cell={cell} />;
    default:
      return;
  }
};

const GridProper = ({ cells, width }) => {
  const handleResize = (layout, oldItem, item) => {
    if (width < 768 || !item || !cells[item.i]) return;
    store.dispatch(
      updateCell({
        uuid: item.i,
        attributes: { x: item.x, y: item.y, w: item.w, h: item.h },
      }),
    );
  };

  return (
    <GridLayout
      className="layout"
      cols={width < 768 ? 1 : 72}
      isDraggable={width >= 768}
      isResizable={width >= 768}
      rowHeight={90}
      width={width - 16}
      onResizeStop={handleResize}
      onDragStop={handleResize}
      draggableHandle=".cell-header"
      draggableCancel="button,input,select,textarea,a"
    >
      {Object.values(cells).map((cell) => (
        <div
          key={cell.uuid}
          uuid={cell.uuid}
          style={{ overflow: "auto" }}
          data-grid={
            width < 768
              ? {
                  x: 0,
                  y: Object.keys(cells).indexOf(cell.uuid) * 12,
                  w: 1,
                  h: 12,
                }
              : cell.attributes
          }
        >
          {renderCellContents(cell)}
        </div>
      ))}
    </GridLayout>
  );
};

const mapStateToProps = (state) => {
  const cells = getCells(state);
  const windowSize = getWindowSize(state);

  return {
    cells: cells,
    width: windowSize.width,
  };
};

export default connect(mapStateToProps, undefined)(GridProper);
