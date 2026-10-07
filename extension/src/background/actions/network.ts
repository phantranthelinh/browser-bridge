import type {
  EmptyResult,
  NetworkFilterArgs,
  NetworkRequestDetailArgs,
  NetworkRequestDetailResult,
  NetworkRequestsResult,
} from '../../generated/protocol';
import type { Handler } from '../router';
import { currentTab, type TabCtx } from './context';

const networkStart: Handler<TabCtx> = async (ctx, args: NetworkFilterArgs): Promise<EmptyResult> => {
  await ctx.network.start(await currentTab(ctx), args.filter);
  return {};
};

const networkRequests: Handler<TabCtx> = async (ctx, args: NetworkFilterArgs): Promise<NetworkRequestsResult> =>
  ctx.network.list(await currentTab(ctx), args.filter);

const networkRequestDetail: Handler<TabCtx> = async (ctx, args: NetworkRequestDetailArgs): Promise<NetworkRequestDetailResult> =>
  ctx.network.detail(await currentTab(ctx), args.requestId);

const networkStop: Handler<TabCtx> = async (ctx): Promise<EmptyResult> => {
  await ctx.network.stop(await currentTab(ctx));
  return {};
};

export const networkHandlers = {
  network_start: networkStart,
  network_requests: networkRequests,
  network_request_detail: networkRequestDetail,
  network_stop: networkStop,
};
