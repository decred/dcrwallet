// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Grpc.Core;
using System;

namespace Paymetheus.Rpc
{
    public static class ErrorHandling
    {
        public static bool IsTransient(Exception ex)
        {
            if (ex is TimeoutException)
                return true;

            var rpcException = ex as RpcException;
            if (rpcException != null && IsTransientStatusCode(rpcException.Status.StatusCode))
                return true;

            var ae = ex as AggregateException;
            return ae != null && IsTransient(ae.Flatten().InnerException);
        }

        private static bool IsTransientStatusCode(StatusCode code)
        {
            switch (code)
            {
                case StatusCode.Cancelled:
                case StatusCode.DeadlineExceeded:
                case StatusCode.ResourceExhausted:
                case StatusCode.Unavailable:
                    return true;
                default:
                    return false;
            }
        }

        public static bool IsServerError(Exception ex)
        {
            var rpcException = ex as RpcException;
            if (rpcException == null)
                return false;

            switch (rpcException.Status.StatusCode)
            {
                case StatusCode.Unknown: // Default code for converted errors
                case StatusCode.NotFound:
                case StatusCode.AlreadyExists:
                case StatusCode.Aborted:
                case StatusCode.Unimplemented:
                case StatusCode.Internal:
                case StatusCode.DataLoss:
                    return true;
                default:
                    return false;
            }
        }

        public static bool IsClientError(Exception ex)
        {
            var rpcException = ex as RpcException;
            if (rpcException == null)
                return false;

            switch (rpcException.Status.StatusCode)
            {
                case StatusCode.InvalidArgument:
                case StatusCode.PermissionDenied:
                case StatusCode.FailedPrecondition:
                case StatusCode.OutOfRange: // RPC parameter
                case StatusCode.Unauthenticated:
                    return true;
                default:
                    return false;
            }
        }
    }

    public static class AggregateExceptionExtensions
    {
        public static bool TryUnwrap(this AggregateException ae, out Exception inner)
        {
            inner = ae.InnerException;
            while (inner is AggregateException)
                inner = inner.InnerException;
            return inner != null;
        }
    }
}
