// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus.Decred.Util
{
    public struct TupleValue<T1, T2>
    {
        public TupleValue(T1 item1, T2 item2)
        {
            Item1 = item1;
            Item2 = item2;
        }

        public T1 Item1 { get; }
        public T2 Item2 { get; }
    }

    public struct TupleValue<T1, T2, T3>
    {
        public TupleValue(T1 item1, T2 item2, T3 item3)
        {
            Item1 = item1;
            Item2 = item2;
            Item3 = item3;
        }

        public T1 Item1 { get; }
        public T2 Item2 { get; }
        public T3 Item3 { get; }
    }

    /// <summary>
    /// TupleValue is an efficient alternative for the System.Tuple types.  TupleValue is a
    /// struct (value type) while Tuple is a class (reference type).  A similar TupleValue struct
    /// and tuple syntax sugar is planned for C# 7, which would eliminate the need for these
    /// types if added.
    /// </summary>
    public static class TupleValue
    {
        public static TupleValue<T1, T2> Create<T1, T2>(T1 item1, T2 item2) =>
            new TupleValue<T1, T2>(item1, item2);

        public static TupleValue<T1, T2, T3> Create<T1, T2, T3>(T1 item1, T2 item2, T3 item3) =>
            new TupleValue<T1, T2, T3>(item1, item2, item3);
    }
}
