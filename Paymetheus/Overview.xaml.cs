using Paymetheus.Framework;
using System;
using System.Collections.Generic;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;

namespace Paymetheus
{
    public partial class Overview
    {
        public Overview()
        {
            InitializeComponent();
        }

        internal Overview(object dataContext) : this()
        {
            DataContext = dataContext;
        }

        private void CopyTxHash_Executed(object sender, ExecutedRoutedEventArgs e)
        {
            Clipboard.SetText(e.Parameter.ToString());
        }
    }
}